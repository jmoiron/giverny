package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

func gravatarHash(email string) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(h[:])
}

// GravatarURI returns the gravatar image URL for the given email if a gravatar
// exists for it, or an empty string if not.
func GravatarURI(email string) string {
	url := fmt.Sprintf("https://gravatar.com/avatar/%s", gravatarHash(email))
	resp, err := http.Get(url + "?d=404")
	if err != nil || resp.StatusCode == http.StatusNotFound {
		return ""
	}
	return url
}
