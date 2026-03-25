package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = bcrypt.DefaultCost

// User is the combined view of monet's user row joined with giverny's user_profile row.
type User struct {
	ID              int64      `db:"id"`
	Username        string     `db:"username"`
	Email           string     `db:"email"`
	Role            string     `db:"role"`
	ProfileImageURI string     `db:"profile_image_uri"`
	CreatedAt       time.Time  `db:"created_at"`
	LastLoginAt     *time.Time `db:"last_login_at"`
}

func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin || u.Role == RoleSuperAdmin
}

func (u *User) IsSuperAdmin() bool {
	return u.Role == RoleSuperAdmin
}

// UserProfileService manages the combined user+user_profile tables.
type UserProfileService struct {
	db db.DB
}

func NewUserProfileService(dbh db.DB) *UserProfileService {
	return &UserProfileService{db: dbh}
}

// CreateUser inserts a monet user row and a giverny user_profile row in one
// transaction. We duplicate the bcrypt logic here rather than calling monet's
// UserService.CreateUser because that method uses its own db handle and cannot
// participate in our transaction.
func (s *UserProfileService) CreateUser(username, email, password, role, profileImageURI string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return err
	}
	return db.With(s.db, func(tx *sqlx.Tx) error {
		res, err := tx.Exec(`INSERT INTO user (username, password_hash) VALUES (?, ?)`, username, string(hashed))
		if err != nil {
			return fmt.Errorf("creating user: %w", err)
		}
		userID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO user_profile (user_id, email, role, profile_image_uri) VALUES (?, ?, ?, ?)`, userID, email, role, profileImageURI)
		return err
	})
}

const userProfileSelect = `SELECT u.id, u.username, p.email, p.role, p.profile_image_uri, p.created_at, p.last_login_at
	FROM user u JOIN user_profile p ON p.user_id = u.id`

func (s *UserProfileService) GetByUsername(username string) (*User, error) {
	var u User
	err := s.db.Get(&u, userProfileSelect+` WHERE u.username=?`, username)
	return &u, err
}

func (s *UserProfileService) GetByEmail(email string) (*User, error) {
	var u User
	err := s.db.Get(&u, userProfileSelect+` WHERE p.email=?`, email)
	return &u, err
}

func (s *UserProfileService) GetByID(id int64) (*User, error) {
	var u User
	err := s.db.Get(&u, userProfileSelect+` WHERE u.id=?`, id)
	return &u, err
}

func (s *UserProfileService) List() ([]*User, error) {
	var users []*User
	err := s.db.Select(&users, userProfileSelect+` ORDER BY u.username`)
	return users, err
}

func (s *UserProfileService) DeleteUser(userID int64) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(`DELETE FROM user_profile WHERE user_id=?`, userID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM user WHERE id=?`, userID)
		return err
	})
}

func (s *UserProfileService) SetRole(userID int64, role string) error {
	_, err := s.db.Exec(`UPDATE user_profile SET role=? WHERE user_id=?`, role, userID)
	return err
}

func (s *UserProfileService) SetProfileImageURI(userID int64, uri string) error {
	_, err := s.db.Exec(`UPDATE user_profile SET profile_image_uri=? WHERE user_id=?`, uri, userID)
	return err
}

func (s *UserProfileService) RecordLogin(userID int64) error {
	_, err := s.db.Exec(`UPDATE user_profile SET last_login_at=datetime('now') WHERE user_id=?`, userID)
	return err
}

// InviteService manages email invitations.
type InviteService struct {
	db db.DB
}

func NewInviteService(dbh db.DB) *InviteService {
	return &InviteService{db: dbh}
}

func randToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Create stores an invitation for email and returns the raw token for the email link.
// Tokens expire after 72 hours.
func (s *InviteService) Create(email string, createdBy int64) (string, error) {
	token, err := randToken()
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(72 * time.Hour)
	_, err = s.db.Exec(
		`INSERT INTO invitation (email, token, created_by, expires_at) VALUES (?, ?, ?, ?)`,
		email, token, createdBy, expires,
	)
	return token, err
}

// GetByToken retrieves an invitation by token without consuming it.
func (s *InviteService) GetByToken(token string) (*Invitation, error) {
	var inv Invitation
	err := s.db.Get(&inv, `SELECT * FROM invitation WHERE token=?`, token)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// Consume validates and marks the invitation used. Returns an error if the token
// is invalid, already used, or expired.
func (s *InviteService) Consume(token string) (*Invitation, error) {
	inv, err := s.GetByToken(token)
	if err != nil {
		return nil, fmt.Errorf("invalid invitation token")
	}
	if inv.UsedAt != nil {
		return nil, fmt.Errorf("invitation has already been used")
	}
	if time.Now().After(inv.ExpiresAt) {
		return nil, fmt.Errorf("invitation has expired")
	}
	now := time.Now()
	if _, err := s.db.Exec(`UPDATE invitation SET used_at=? WHERE id=?`, now, inv.ID); err != nil {
		return nil, err
	}
	inv.UsedAt = &now
	return inv, nil
}
