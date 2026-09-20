package mobile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/jmoiron/giverny/conf"
	"github.com/jmoiron/giverny/kanban"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
)

var PushMigrations = monarch.Set{
	Name: "mobile_push",
	Migrations: []monarch.Migration{{
		Up: `CREATE TABLE IF NOT EXISTS push_subscription (
			id INTEGER NOT NULL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
			endpoint TEXT NOT NULL UNIQUE,
			p256dh TEXT NOT NULL,
			auth TEXT NOT NULL,
			created_at DATETIME DEFAULT (datetime('now')),
			last_used_at DATETIME
		);`,
		Down: `DROP TABLE push_subscription;`,
	}, {
		Up: `CREATE TABLE IF NOT EXISTS push_vapid_key (
			id INTEGER NOT NULL PRIMARY KEY CHECK (id=1),
			public_key TEXT NOT NULL,
			private_key TEXT NOT NULL,
			created_at DATETIME DEFAULT (datetime('now'))
		);`,
		Down: `DROP TABLE push_vapid_key;`,
	}},
}

type pushSubscription struct {
	ID        int64      `db:"id"`
	UserID    int64      `db:"user_id"`
	Endpoint  string     `db:"endpoint"`
	P256DH    string     `db:"p256dh"`
	Auth      string     `db:"auth"`
	CreatedAt time.Time  `db:"created_at"`
	LastUsed  *time.Time `db:"last_used_at"`
}

type pushSubscriptionService struct{ db db.DB }

func (s *pushSubscriptionService) Save(userID int64, sub webpush.Subscription) error {
	if strings.TrimSpace(sub.Endpoint) == "" || sub.Keys.P256dh == "" || sub.Keys.Auth == "" {
		return fmt.Errorf("incomplete push subscription")
	}
	_, err := s.db.Exec(`INSERT INTO push_subscription (user_id, endpoint, p256dh, auth) VALUES (?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET user_id=excluded.user_id, p256dh=excluded.p256dh, auth=excluded.auth`, userID, sub.Endpoint, sub.Keys.P256dh, sub.Keys.Auth)
	return err
}

func (s *pushSubscriptionService) Delete(userID int64, endpoint string) error {
	_, err := s.db.Exec(`DELETE FROM push_subscription WHERE user_id=? AND endpoint=?`, userID, endpoint)
	return err
}

func (s *pushSubscriptionService) ListByUser(userID int64) ([]pushSubscription, error) {
	var subscriptions []pushSubscription
	err := s.db.Select(&subscriptions, `SELECT id, user_id, endpoint, p256dh, auth, created_at, last_used_at FROM push_subscription WHERE user_id=?`, userID)
	return subscriptions, err
}

type vapidService struct {
	db     db.DB
	secret string
	mu     sync.Mutex
}

func (s *vapidService) key() ([]byte, error) {
	hash := sha256.Sum256([]byte(s.secret))
	return hash[:], nil
}

func (s *vapidService) encrypt(value string) (string, error) {
	key, _ := s.key()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (s *vapidService) decrypt(value string) (string, error) {
	key, _ := s.key()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil || len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid VAPID key")
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *vapidService) Ensure() (publicKey, privateKey string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var encrypted string
	err = s.db.Get(&publicKey, `SELECT public_key FROM push_vapid_key WHERE id=1`)
	if err == nil {
		err = s.db.Get(&encrypted, `SELECT private_key FROM push_vapid_key WHERE id=1`)
		if err != nil {
			return "", "", err
		}
		privateKey, err = s.decrypt(encrypted)
		return
	}
	if err != sql.ErrNoRows {
		return "", "", err
	}
	privateKey, publicKey, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", err
	}
	encrypted, err = s.encrypt(privateKey)
	if err != nil {
		return "", "", err
	}
	_, err = s.db.Exec(`INSERT INTO push_vapid_key (id, public_key, private_key) VALUES (1, ?, ?)`, publicKey, encrypted)
	return
}

type PushService struct {
	db     db.DB
	cfg    *conf.Config
	subs   *pushSubscriptionService
	vapid  *vapidService
	kanban *kanban.App
}

func NewPushService(dbh db.DB, cfg *conf.Config, kanbanApp *kanban.App) *PushService {
	return &PushService{db: dbh, cfg: cfg, kanban: kanbanApp, subs: &pushSubscriptionService{db: dbh}, vapid: &vapidService{db: dbh, secret: cfg.Secret}}
}

func (p *PushService) NotifyCardAssigned(cardID, assigneeID int64) {
	p.notify([]int64{assigneeID}, cardID, "card assigned to you")
}

func (p *PushService) NotifyNewComment(cardID, actorID int64) {
	recipients, err := p.kanban.Cards().NotificationRecipients(cardID)
	if err != nil {
		return
	}
	filtered := recipients[:0]
	for _, id := range recipients {
		if id != actorID {
			filtered = append(filtered, id)
		}
	}
	p.notify(filtered, cardID, "new comment on a card you follow")
}

func (p *PushService) notify(userIDs []int64, cardID int64, message string) {
	card, err := p.kanban.Cards().Get(cardID)
	if err != nil {
		return
	}
	board, err := p.kanban.Boards().Get(card.BoardID)
	if err != nil {
		return
	}
	publicKey, privateKey, err := p.vapid.Ensure()
	if err != nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{"title": "Giverny", "body": message + ": " + card.Title, "url": "/mobile/boards/" + board.Slug + "/cards/" + fmt.Sprint(card.ID) + "/"})
	for _, userID := range userIDs {
		subscriptions, err := p.subs.ListByUser(userID)
		if err != nil {
			continue
		}
		for _, stored := range subscriptions {
			_, err := webpush.SendNotification(payload, &webpush.Subscription{Endpoint: stored.Endpoint, Keys: webpush.Keys{P256dh: stored.P256DH, Auth: stored.Auth}}, &webpush.Options{Subscriber: p.cfg.BaseURL, VAPIDPublicKey: publicKey, VAPIDPrivateKey: privateKey, TTL: 3600})
			if err == nil {
				_, _ = p.db.Exec(`UPDATE push_subscription SET last_used_at=datetime('now') WHERE id=?`, stored.ID)
			}
		}
	}
}

var _ kanban.PushNotifier = (*PushService)(nil)
