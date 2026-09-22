package kanban

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
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

func (s *NotificationService) DeleteRead(userID int64) ([]int64, error) {
	var ids []int64
	if err := s.db.Select(&ids, `SELECT id FROM user_notification WHERE user_id=? AND read_at IS NOT NULL`, userID); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return ids, nil
	}
	_, err := s.db.Exec(`DELETE FROM user_notification WHERE user_id=? AND read_at IS NOT NULL`, userID)
	return ids, err
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
		(*notifications)[i].TitleDiff = notificationDiff((*notifications)[i].OldTitle, (*notifications)[i].NewTitle)
		(*notifications)[i].ContentDiff = notificationDiff((*notifications)[i].OldContent, (*notifications)[i].NewContent)
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

func notificationDiff(oldValue, newValue string) []NotificationDiffLine {
	if oldValue == newValue {
		return nil
	}
	edits := myers.ComputeEdits(span.URIFromPath("old"), oldValue, newValue)
	diff := fmt.Sprint(gotextdiff.ToUnified("old", "new", oldValue, edits))
	lines := strings.Split(strings.TrimSuffix(diff, "\n"), "\n")
	result := make([]NotificationDiffLine, 0, len(lines))
	for _, line := range lines {
		class := "diff-context"
		switch {
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			class = "diff-file-header"
		case strings.HasPrefix(line, "@@"):
			class = "diff-hunk-header"
		case strings.HasPrefix(line, "-"):
			class = "diff-removed"
		case strings.HasPrefix(line, "+"):
			class = "diff-added"
		}
		if class == "diff-removed" || class == "diff-added" {
			result = append(result, NotificationDiffLine{Class: class, Text: line})
		}
	}
	for i := 0; i < len(result); i++ {
		if result[i].Class != "diff-removed" {
			continue
		}
		added := i + 1
		if added >= len(result) || result[added].Class != "diff-added" {
			continue
		}
		oldParts, newParts := inlineDiff(strings.TrimPrefix(result[i].Text, "-"), strings.TrimPrefix(result[added].Text, "+"))
		result[i].Text, result[i].Prefix, result[i].Parts = "", "-", oldParts
		result[added].Text, result[added].Prefix, result[added].Parts = "", "+", newParts
	}
	return result
}

func inlineDiff(oldValue, newValue string) ([]NotificationDiffPart, []NotificationDiffPart) {
	oldTokens, newTokens := diffTokens(oldValue), diffTokens(newValue)
	if len(oldTokens) > 1000 || len(newTokens) > 1000 {
		return []NotificationDiffPart{{Class: "diff-inline-removed", Text: oldValue}}, []NotificationDiffPart{{Class: "diff-inline-added", Text: newValue}}
	}

	width := len(newTokens) + 1
	dp := make([]int, (len(oldTokens)+1)*width)
	for i := len(oldTokens) - 1; i >= 0; i-- {
		for j := len(newTokens) - 1; j >= 0; j-- {
			if oldTokens[i] == newTokens[j] {
				dp[i*width+j] = dp[(i+1)*width+j+1] + 1
			} else if dp[(i+1)*width+j] >= dp[i*width+j+1] {
				dp[i*width+j] = dp[(i+1)*width+j]
			} else {
				dp[i*width+j] = dp[i*width+j+1]
			}
		}
	}

	oldParts := make([]NotificationDiffPart, 0)
	newParts := make([]NotificationDiffPart, 0)
	add := func(parts *[]NotificationDiffPart, class, text string) {
		if text == "" {
			return
		}
		if len(*parts) > 0 && (*parts)[len(*parts)-1].Class == class {
			(*parts)[len(*parts)-1].Text += text
			return
		}
		*parts = append(*parts, NotificationDiffPart{Class: class, Text: text})
	}
	for i, j := 0, 0; i < len(oldTokens) || j < len(newTokens); {
		switch {
		case i < len(oldTokens) && j < len(newTokens) && oldTokens[i] == newTokens[j]:
			add(&oldParts, "diff-inline-context", oldTokens[i])
			add(&newParts, "diff-inline-context", newTokens[j])
			i++
			j++
		case i < len(oldTokens) && (j == len(newTokens) || dp[(i+1)*width+j] >= dp[i*width+j+1]):
			add(&oldParts, "diff-inline-removed", oldTokens[i])
			i++
		default:
			add(&newParts, "diff-inline-added", newTokens[j])
			j++
		}
	}
	return oldParts, newParts
}

func diffTokens(value string) []string {
	runes := []rune(value)
	if len(runes) == 0 {
		return nil
	}
	tokens := make([]string, 0, len(runes))
	start := 0
	category := func(r rune) int {
		switch {
		case unicode.IsSpace(r):
			return 0
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			return 1
		default:
			return 2
		}
	}
	for i := 1; i <= len(runes); i++ {
		if i == len(runes) || category(runes[i]) != category(runes[start]) {
			tokens = append(tokens, string(runes[start:i]))
			start = i
		}
	}
	return tokens
}
