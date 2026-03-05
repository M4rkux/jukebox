package middleware

import (
	"net/http"

	"github.com/yourusername/jukebox/internal/auth"
)

// Session injects session info without requiring auth
func Session(store *auth.SessionStore, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next(w, r)
	})
}

// RequireAuth redirects to home if not authenticated
func RequireAuth(store *auth.SessionStore, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := store.GetUserID(r); !ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		next(w, r)
	})
}
