package kanban

import (
	"github.com/jmoiron/monet/db"
)

type NotificationService struct{ db db.DB }

func NewNotificationService(dbh db.DB) *NotificationService { return &NotificationService{db: dbh} }

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

func (s *NotificationService) Create(userID, actorID, boardID, cardID int64, notificationType NotificationType, url string) error {
	_, err := s.db.Exec(`INSERT INTO user_notification (user_id, actor_id, board_id, card_id, notification_type, message, url) VALUES (?, ?, ?, ?, ?, ?, ?)`, userID, actorID, boardID, cardID, notificationType, "", url)
	return err
}

func (s *NotificationService) RecentForUser(userID int64, limit int) ([]UserNotification, error) {
	if limit <= 0 {
		limit = 20
	}
	var notifications []UserNotification
	err := s.db.Select(&notifications, `SELECT id, actor_id, board_id, card_id, notification_type, message, url, created_at FROM user_notification WHERE user_id=? ORDER BY created_at DESC, id DESC LIMIT ?`, userID, limit)
	return notifications, err
}
