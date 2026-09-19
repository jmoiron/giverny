package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/jmoiron/monet/app"
	mauth "github.com/jmoiron/monet/auth"
)

const (
	webAuthnRegistrationSession = "giverny_webauthn_registration"
	webAuthnLoginSession        = "giverny_webauthn_login"
)

func (a *App) webAuthnReady(w http.ResponseWriter) bool {
	if a.webAuthnErr != nil {
		http.Error(w, "passkeys are not configured", http.StatusInternalServerError)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		app.Http500("writing JSON response", w, err)
	}
}

func storeWebAuthnSession(r *http.Request, w http.ResponseWriter, key string, data *webauthn.SessionData) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	session := mauth.SessionFromContext(r.Context()).Session(r)
	session.Values[key] = string(payload)
	return session.Save(r, w)
}

func loadWebAuthnSession(r *http.Request, key string) (webauthn.SessionData, error) {
	session := mauth.SessionFromContext(r.Context()).Session(r)
	raw, ok := session.Values[key].(string)
	if !ok || raw == "" {
		return webauthn.SessionData{}, fmt.Errorf("passkey ceremony has expired")
	}
	delete(session.Values, key)
	var data webauthn.SessionData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return data, fmt.Errorf("decoding passkey ceremony: %w", err)
	}
	return data, nil
}

func (a *App) handleWebAuthnRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if !a.webAuthnReady(w) {
		return
	}
	user := UserFromContext(r.Context())
	waUser, err := a.users.webAuthnUser(user)
	if err != nil {
		app.Http500("loading passkeys", w, err)
		return
	}
	creation, session, err := a.webAuthn.BeginRegistration(waUser)
	if err != nil {
		app.Http500("starting passkey registration", w, err)
		return
	}
	if err := storeWebAuthnSession(r, w, webAuthnRegistrationSession, session); err != nil {
		app.Http500("storing passkey registration", w, err)
		return
	}
	writeJSON(w, creation)
}

func (a *App) handleWebAuthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if !a.webAuthnReady(w) {
		return
	}
	user := UserFromContext(r.Context())
	waUser, err := a.users.webAuthnUser(user)
	if err != nil {
		app.Http500("loading passkeys", w, err)
		return
	}
	session, err := loadWebAuthnSession(r, webAuthnRegistrationSession)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	credential, err := a.webAuthn.FinishRegistration(waUser, session, r)
	if err != nil {
		http.Error(w, "passkey registration failed", http.StatusBadRequest)
		return
	}
	if err := a.users.createWebAuthnCredential(user.ID, credential, ""); err != nil {
		app.Http500("saving passkey", w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (a *App) handleWebAuthnCredentials(w http.ResponseWriter, r *http.Request) {
	rows, err := a.users.listWebAuthnCredentials(UserFromContext(r.Context()).ID)
	if err != nil {
		app.Http500("listing passkeys", w, err)
		return
	}
	writeJSON(w, rows)
}

func (a *App) handleWebAuthnCredentialDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid passkey", http.StatusBadRequest)
		return
	}
	if err := a.users.deleteWebAuthnCredential(UserFromContext(r.Context()).ID, id); err != nil {
		app.Http500("deleting passkey", w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (a *App) handleWebAuthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !a.webAuthnReady(w) {
		return
	}
	assertion, session, err := a.webAuthn.BeginDiscoverableLogin()
	if err != nil {
		app.Http500("starting passkey login", w, err)
		return
	}
	if err := storeWebAuthnSession(r, w, webAuthnLoginSession, session); err != nil {
		app.Http500("storing passkey login", w, err)
		return
	}
	writeJSON(w, assertion)
}

func (a *App) handleWebAuthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !a.webAuthnReady(w) {
		return
	}
	session, err := loadWebAuthnSession(r, webAuthnLoginSession)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	user, credential, err := a.webAuthn.FinishPasskeyLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		profile, err := a.users.webAuthnCredentialOwner(rawID)
		if err != nil {
			return nil, err
		}
		if len(userHandle) > 0 && !bytes.Equal(userHandle, []byte(strconv.FormatInt(profile.ID, 10))) {
			return nil, fmt.Errorf("passkey user handle mismatch")
		}
		return a.users.webAuthnUser(profile)
	}, session, r)
	if err != nil {
		http.Error(w, "passkey login failed", http.StatusUnauthorized)
		return
	}
	profile := user.(*webAuthnUser).profile
	if err := a.users.updateWebAuthnCredentialForUser(profile.ID, credential); err != nil {
		app.Http500("updating passkey", w, err)
		return
	}
	sm := mauth.SessionFromContext(r.Context())
	cookieSession := sm.Session(r)
	cookieSession.Values["authenticated"] = true
	cookieSession.Values["user"] = profile.Username
	if err := cookieSession.Save(r, w); err != nil {
		app.Http500("saving login session", w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
