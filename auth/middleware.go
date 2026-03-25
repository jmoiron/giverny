package auth

import (
	"context"
	"net/http"

	"github.com/jmoiron/monet/auth"
)

type userKey struct{}

// AddUserMiddleware looks up the logged-in user from the session and stores
// the *User in the request context. If no session or user is found, the
// request continues with no user in context (anonymous).
func AddUserMiddleware(svc *UserProfileService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sm := auth.SessionFromContext(r.Context())
			if sm.IsAuthenticated(r) {
				session := sm.Session(r)
				username, _ := session.Values["user"].(string)
				if user, err := svc.GetByUsername(username); err == nil {
					r = r.WithContext(context.WithValue(r.Context(), userKey{}, user))
					if session.Values["login_recorded"] != true {
						session.Values["login_recorded"] = true
						session.Save(r, w)
						svc.RecordLogin(user.ID)
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserFromContext returns the authenticated *User from the context, or nil.
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userKey{}).(*User)
	return u
}

// RequireAuth returns 401 if the request has no authenticated user.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) == nil {
			http.Redirect(w, r, "/login/", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin returns 403 if the user is not an admin or superadmin.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			http.Redirect(w, r, "/login/", http.StatusFound)
			return
		}
		if !u.IsAdmin() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSuperAdmin returns 403 if the user is not a superadmin.
func RequireSuperAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			http.Redirect(w, r, "/login/", http.StatusFound)
			return
		}
		if !u.IsSuperAdmin() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
