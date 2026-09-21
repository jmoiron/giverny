package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	webauthn "github.com/go-webauthn/webauthn/webauthn"
)

type WebAuthnCredential struct {
	ID              int64      `db:"id" json:"id"`
	UserID          int64      `db:"user_id" json:"-"`
	CredentialID    string     `db:"credential_id" json:"credential_id"`
	PublicKey       []byte     `db:"public_key" json:"-"`
	AttestationType string     `db:"attestation_type" json:"-"`
	Transports      string     `db:"transports" json:"-"`
	SignCount       uint32     `db:"sign_count" json:"-"`
	CloneWarning    bool       `db:"clone_warning" json:"-"`
	AAGUID          string     `db:"aaguid" json:"-"`
	CredentialJSON  string     `db:"credential_json" json:"-"`
	Name            string     `db:"name" json:"name"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	LastUsedAt      *time.Time `db:"last_used_at" json:"last_used_at"`
}

type webAuthnUser struct {
	profile     *User
	credentials []webauthn.Credential
}

func (u *webAuthnUser) WebAuthnID() []byte   { return []byte(strconv.FormatInt(u.profile.ID, 10)) }
func (u *webAuthnUser) WebAuthnName() string { return u.profile.Username }
func (u *webAuthnUser) WebAuthnDisplayName() string {
	if u.profile.Email != "" {
		return u.profile.Email
	}
	return u.profile.Username
}
func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func newWebAuthn(baseURL string) (*webauthn.WebAuthn, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("base URL has no host")
	}
	return webauthn.New(&webauthn.Config{
		RPID:          u.Hostname(),
		RPDisplayName: "Giverny",
		RPOrigins:     []string{stringsTrimRight(baseURL)},
	})
}

func stringsTrimRight(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func (s *UserProfileService) listWebAuthnCredentials(userID int64) ([]WebAuthnCredential, error) {
	// Keep the JSON representation stable for clients: a user with no
	// passkeys should receive [] rather than null.
	rows := make([]WebAuthnCredential, 0)
	err := s.db.Select(&rows, `SELECT id, user_id, credential_id, public_key, attestation_type, transports, sign_count, clone_warning, aaguid, credential_json, name, created_at, last_used_at FROM webauthn_credential WHERE user_id=? ORDER BY created_at, id`, userID)
	return rows, err
}

func (s *UserProfileService) webAuthnUser(user *User) (*webAuthnUser, error) {
	rows, err := s.listWebAuthnCredentials(user.ID)
	if err != nil {
		return nil, err
	}
	credentials := make([]webauthn.Credential, 0, len(rows))
	for _, row := range rows {
		var credential webauthn.Credential
		if err := json.Unmarshal([]byte(row.CredentialJSON), &credential); err != nil {
			return nil, fmt.Errorf("decoding passkey %d: %w", row.ID, err)
		}
		credentials = append(credentials, credential)
	}
	return &webAuthnUser{profile: user, credentials: credentials}, nil
}

func (s *UserProfileService) createWebAuthnCredential(userID int64, credential *webauthn.Credential, name string) error {
	payload, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO webauthn_credential (user_id, credential_id, public_key, attestation_type, transports, sign_count, clone_warning, aaguid, credential_json, name) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, userID, base64.RawURLEncoding.EncodeToString(credential.ID), credential.PublicKey, credential.AttestationType, "", credential.Authenticator.SignCount, credential.Authenticator.CloneWarning, base64.RawURLEncoding.EncodeToString(credential.Authenticator.AAGUID), string(payload), name)
	return err
}

func (s *UserProfileService) updateWebAuthnCredential(credential *webauthn.Credential) error {
	payload, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE webauthn_credential SET credential_json=?, last_used_at=datetime('now') WHERE credential_id=?`, string(payload), base64.RawURLEncoding.EncodeToString(credential.ID))
	return err
}

func (s *UserProfileService) deleteWebAuthnCredential(userID, credentialID int64) error {
	_, err := s.db.Exec(`DELETE FROM webauthn_credential WHERE id=? AND user_id=?`, credentialID, userID)
	return err
}

func (s *UserProfileService) webAuthnCredentialOwner(rawID []byte) (*User, error) {
	var userID int64
	err := s.db.Get(&userID, `SELECT user_id FROM webauthn_credential WHERE credential_id=?`, base64.RawURLEncoding.EncodeToString(rawID))
	if err != nil {
		return nil, err
	}
	return s.GetByID(userID)
}

func (s *UserProfileService) updateWebAuthnCredentialForUser(userID int64, credential *webauthn.Credential) error {
	var owner int64
	if err := s.db.Get(&owner, `SELECT user_id FROM webauthn_credential WHERE credential_id=?`, base64.RawURLEncoding.EncodeToString(credential.ID)); err != nil {
		return err
	}
	if owner != userID {
		return fmt.Errorf("passkey does not belong to user")
	}
	return s.updateWebAuthnCredential(credential)
}
