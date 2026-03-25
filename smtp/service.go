package smtp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/smtp"

	"github.com/jmoiron/monet/db"
)

// Service manages SMTP configuration and sending.
type Service struct {
	db  db.DB
	key []byte // 32-byte AES-256 key derived from conf.Secret
}

func NewService(dbh db.DB, secret string) (*Service, error) {
	key, err := hex.DecodeString(secret)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("smtp: secret must be a 64-char hex string (32 bytes); got len=%d", len(key))
	}
	return &Service{db: dbh, key: key}, nil
}

// encrypt encrypts plaintext with AES-256-GCM and returns a hex-encoded ciphertext.
func (s *Service) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.key)
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
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// decrypt decrypts a hex-encoded AES-256-GCM ciphertext.
func (s *Service) decrypt(encoded string) (string, error) {
	data, err := hex.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// Get returns the current SMTP config row, inserting a default if none exists.
func (s *Service) Get() (*Config, error) {
	var cfg Config
	err := s.db.Get(&cfg, `SELECT * FROM smtp_config LIMIT 1`)
	if err != nil {
		_, err = s.db.Exec(`INSERT INTO smtp_config (host) VALUES ('')`)
		if err != nil {
			return nil, err
		}
		err = s.db.Get(&cfg, `SELECT * FROM smtp_config LIMIT 1`)
	}
	return &cfg, err
}

// Save updates the SMTP config. If password is non-empty it is encrypted and
// stored; if empty the existing encrypted password is left unchanged.
func (s *Service) Save(host string, port int, username, password, fromAddress string) error {
	cfg, err := s.Get()
	if err != nil {
		return err
	}
	encrypted := cfg.EncryptedPassword
	if password != "" {
		encrypted, err = s.encrypt(password)
		if err != nil {
			return fmt.Errorf("encrypting password: %w", err)
		}
	}
	_, err = s.db.Exec(`UPDATE smtp_config SET host=?, port=?, username=?, encrypted_password=?, from_address=?, updated_at=datetime('now') WHERE id=?`,
		host, port, username, encrypted, fromAddress, cfg.ID)
	return err
}

// Send opens a connection and sends a plain-text email.
func (s *Service) Send(to, subject, body string) error {
	cfg, err := s.Get()
	if err != nil {
		return err
	}
	if cfg.Host == "" {
		return fmt.Errorf("smtp not configured")
	}
	password, err := s.decrypt(cfg.EncryptedPassword)
	if err != nil {
		return fmt.Errorf("decrypting smtp password: %w", err)
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	auth := smtp.PlainAuth("", cfg.Username, password, cfg.Host)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s", cfg.FromAddress, to, subject, body)
	return smtp.SendMail(addr, auth, cfg.FromAddress, []string{to}, []byte(msg))
}
