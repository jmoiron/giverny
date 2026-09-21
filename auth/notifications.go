package auth

import (
	"database/sql"
	"time"
)

const (
	NotificationPush       = "push"
	NotificationEmail      = "email_digest"
	NotificationPushEmail  = "push_and_digest"
	NotificationDisabled   = "disabled"
	NotificationNewCard    = "new_card"
	NotificationCardClosed = "card_closed"
	NotificationCardUpdated = "card_updated"
	NotificationCardAssigned = "card_assigned"
	NotificationCardComment = "card_comment"
)

type NotificationSettings struct {
	DeliveryMode string `db:"delivery_mode"`
	NewCard      bool   `db:"new_card"`
	CardClosed   bool   `db:"card_closed"`
	CardUpdated  bool   `db:"card_updated"`
	CardAssigned bool   `db:"card_assigned"`
	CardComment  bool   `db:"card_comment"`
}

type UserNotification struct {
	ID        int64     `db:"id"`
	Message   string    `db:"message"`
	URL       string    `db:"url"`
	CreatedAt time.Time `db:"created_at"`
}

func defaultNotificationSettings() NotificationSettings {
	return NotificationSettings{DeliveryMode: NotificationPush, NewCard: true, CardClosed: true, CardUpdated: true, CardAssigned: true, CardComment: true}
}

func (s *UserProfileService) GetNotificationSettings(userID int64) (NotificationSettings, error) {
	settings := NotificationSettings{}
	err := s.db.Get(&settings, `SELECT delivery_mode, new_card, card_closed, card_updated, card_assigned, card_comment FROM notification_setting WHERE user_id=?`, userID)
	if err == sql.ErrNoRows {
		settings = defaultNotificationSettings()
		_, err = s.db.Exec(`INSERT INTO notification_setting (user_id, delivery_mode, new_card, card_closed, card_updated, card_assigned, card_comment) VALUES (?, ?, ?, ?, ?, ?, ?)`, userID, settings.DeliveryMode, settings.NewCard, settings.CardClosed, settings.CardUpdated, settings.CardAssigned, settings.CardComment)
	}
	return settings, err
}

func (s *UserProfileService) UpdateNotificationSettings(userID int64, settings NotificationSettings) error {
	if settings.DeliveryMode != NotificationPush && settings.DeliveryMode != NotificationEmail && settings.DeliveryMode != NotificationPushEmail && settings.DeliveryMode != NotificationDisabled {
		settings.DeliveryMode = NotificationPush
	}
	_, err := s.db.Exec(`INSERT INTO notification_setting (user_id, delivery_mode, new_card, card_closed, card_updated, card_assigned, card_comment)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET delivery_mode=excluded.delivery_mode, new_card=excluded.new_card, card_closed=excluded.card_closed, card_updated=excluded.card_updated, card_assigned=excluded.card_assigned, card_comment=excluded.card_comment`,
		userID, settings.DeliveryMode, settings.NewCard, settings.CardClosed, settings.CardUpdated, settings.CardAssigned, settings.CardComment)
	return err
}

func (s *UserProfileService) BoardNotificationsEnabled(userID, boardID int64) (bool, error) {
	var count int
	err := s.db.Get(&count, `SELECT COUNT(*) FROM notification_board_mute WHERE user_id=? AND board_id=?`, userID, boardID)
	return count == 0, err
}

func (s *UserProfileService) SetBoardNotifications(userID, boardID int64, enabled bool) error {
	if enabled {
		_, err := s.db.Exec(`DELETE FROM notification_board_mute WHERE user_id=? AND board_id=?`, userID, boardID)
		return err
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO notification_board_mute (user_id, board_id) VALUES (?, ?)`, userID, boardID)
	return err
}

func (s *UserProfileService) CreateNotification(userID int64, message, url string) error {
	_, err := s.db.Exec(`INSERT INTO user_notification (user_id, message, url) VALUES (?, ?, ?)`, userID, message, url)
	return err
}

func (s *UserProfileService) RecentNotifications(userID int64, limit int) ([]UserNotification, error) {
	if limit <= 0 {
		limit = 20
	}
	var notifications []UserNotification
	err := s.db.Select(&notifications, `
		SELECT id, message, url, created_at
		FROM user_notification
		WHERE user_id=?
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, userID, limit)
	return notifications, err
}
