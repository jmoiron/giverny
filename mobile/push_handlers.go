package mobile

import (
	"encoding/json"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/jmoiron/giverny/auth"
)

func (a *App) handleVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	publicKey, _, err := a.push.vapid.Ensure()
	if err != nil {
		http.Error(w, "push notifications are not configured", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"publicKey": publicKey})
}

func (a *App) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	var sub webpush.Subscription
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		http.Error(w, "invalid push subscription", http.StatusBadRequest)
		return
	}
	if err := a.push.subs.Save(auth.UserFromContext(r.Context()).ID, sub); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writePushJSON(w, map[string]bool{"ok": true})
}

func (a *App) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Endpoint == "" {
		http.Error(w, "invalid push subscription", http.StatusBadRequest)
		return
	}
	if err := a.push.subs.Delete(auth.UserFromContext(r.Context()).ID, request.Endpoint); err != nil {
		http.Error(w, "could not remove push subscription", http.StatusInternalServerError)
		return
	}
	writePushJSON(w, map[string]bool{"ok": true})
}

func writePushJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
