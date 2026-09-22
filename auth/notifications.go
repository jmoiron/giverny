package auth

import "database/sql"

const (
	NotificationPush         = "push"
	NotificationEmail        = "email_digest"
	NotificationPushEmail    = "push_and_digest"
	NotificationDisabled     = "disabled"
	NotificationNewCard      = "new_card"
	NotificationCardClosed   = "card_closed"
	NotificationCardUpdated  = "card_updated"
	NotificationCardAssigned = "card_assigned"
	NotificationCardComment  = "card_comment"
)

type NotificationSettings struct {
	DeliveryMode string `db:"delivery_mode"`
	NewCard      bool   `db:"new_card"`
	CardClosed   bool   `db:"card_closed"`
	CardUpdated  bool   `db:"card_updated"`
	CardAssigned bool   `db:"card_assigned"`
	CardComment  bool   `db:"card_comment"`
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
