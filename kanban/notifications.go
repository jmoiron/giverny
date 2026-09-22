package kanban

import (
	"database/sql"

	gauth "github.com/jmoiron/giverny/auth"
	"github.com/jmoiron/monet/db"
)

type NotificationService struct {
	db     db.DB
	boards *BoardService
	cards  *CardService
	users  *gauth.UserProfileService
}

func NewNotificationService(dbh db.DB, boards *BoardService, cards *CardService) *NotificationService {
	return &NotificationService{db: dbh, boards: boards, cards: cards, users: gauth.NewUserProfileService(dbh)}
}

func (s *NotificationService) BoardEnabled(userID, boardID int64) (bool, error) {
	var count int
	err := s.db.Get(&count, `SELECT COUNT(*) FROM notification_board_mute WHERE user_id=? AND board_id=?`, userID, boardID)
	return count == 0, err
}

func (s *NotificationService) SetBoardEnabled(userID, boardID int64, enabled bool) error {
	if enabled {
		_, err := s.db.Exec(`DELETE FROM notification_board_mute WHERE user_id=? AND board_id=?`, userID, boardID)
		return err
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO notification_board_mute (user_id, board_id) VALUES (?, ?)`, userID, boardID)
	return err
}

type NotificationChange struct {
	OldTitle   string
	NewTitle   string
	OldContent string
	NewContent string
}

func (s *NotificationService) Create(userID, actorID, boardID, cardID int64, notificationType NotificationType, url string, change NotificationChange) (int64, error) {
	result, err := s.db.Exec(`INSERT INTO user_notification (user_id, actor_id, board_id, card_id, notification_type, message, url, old_title, new_title, old_content, new_content) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, userID, actorID, boardID, cardID, notificationType, "", url, change.OldTitle, change.NewTitle, change.OldContent, change.NewContent)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return id, err
}

func (s *NotificationService) MarkRead(userID, notificationID int64) error {
	_, err := s.db.Exec(`UPDATE user_notification SET read_at=datetime('now') WHERE id=? AND user_id=? AND read_at IS NULL`, notificationID, userID)
	return err
}

func (s *NotificationService) MarkAllRead(userID int64) error {
	_, err := s.db.Exec(`UPDATE user_notification SET read_at=datetime('now') WHERE user_id=? AND read_at IS NULL`, userID)
	return err
}

func (s *NotificationService) DeleteRead(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM user_notification WHERE user_id=? AND read_at IS NOT NULL`, userID)
	return err
}

func (s *NotificationService) UnreadCount(userID int64) (int, error) {
	var count int
	err := s.db.Get(&count, `SELECT COUNT(*) FROM user_notification WHERE user_id=? AND read_at IS NULL`, userID)
	return count, err
}

func (s *NotificationService) RecentForUser(userID int64, limit int) ([]UserNotification, error) {
	return s.RecentForUserPage(userID, limit, 0)
}

func (s *NotificationService) RecentForUserPage(userID int64, limit, offset int) ([]UserNotification, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var notifications []UserNotification
	err := s.db.Select(&notifications, `SELECT id, actor_id, board_id, card_id, notification_type, message, url, created_at, read_at, old_title, new_title, old_content, new_content FROM user_notification WHERE user_id=? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	s.enrich(&notifications)
	return notifications, nil
}

func (s *NotificationService) GetForUser(userID, notificationID int64) (*UserNotification, error) {
	var notifications []UserNotification
	err := s.db.Select(&notifications, `SELECT id, actor_id, board_id, card_id, notification_type, message, url, created_at, read_at, old_title, new_title, old_content, new_content FROM user_notification WHERE user_id=? AND id=?`, userID, notificationID)
	if err != nil {
		return nil, err
	}
	if len(notifications) == 0 {
		return nil, sql.ErrNoRows
	}
	s.enrich(&notifications)
	return &notifications[0], nil
}

func (s *NotificationService) CountForUser(userID int64) (int, error) {
	var count int
	err := s.db.Get(&count, `SELECT COUNT(*) FROM user_notification WHERE user_id=?`, userID)
	return count, err
}

func (s *NotificationService) enrich(notifications *[]UserNotification) {
	for i := range *notifications {
		if s.cards != nil && (*notifications)[i].CardID != 0 {
			(*notifications)[i].Card, _ = s.cards.Get((*notifications)[i].CardID)
		}
		if s.boards != nil && (*notifications)[i].BoardID != 0 {
			(*notifications)[i].Board, _ = s.boards.Get((*notifications)[i].BoardID)
		}
		if s.users != nil && (*notifications)[i].ActorID != 0 {
			(*notifications)[i].Actor, _ = s.users.GetByID((*notifications)[i].ActorID)
		}
	}
}
