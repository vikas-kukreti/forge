package auth

import (
	"context"
	"net/http"

	"forge/internal/store"
	"forge/internal/types"
)

type contextKey string

const UserIDKey contextKey = "user_id"

// CSRFMiddleware enforces X-Forge-CSRF on mutating requests
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" || r.Method == "DELETE" {
			if r.Header.Get("X-Forge-CSRF") != "1" {
				types.WriteError(w, types.ErrForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// SessionMiddleware validates the forge_session cookie
func SessionMiddleware(sessionStore *store.SessionStore, userStore *store.UserStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("forge_session")
			if err != nil {
				types.WriteError(w, types.ErrUnauthorized)
				return
			}

			tokenHash := HashToken(cookie.Value)
			userID, err := sessionStore.GetSessionUser(r.Context(), tokenHash)
			if err != nil || userID == "" {
				types.WriteError(w, types.ErrUnauthorized)
				return
			}

			// check if suspended
			user, err := userStore.GetUserByID(r.Context(), userID)
			if err != nil || user == nil || user.Status == "suspended" {
				types.WriteError(w, types.ErrForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AdminMiddleware ensures the user is an admin
func AdminMiddleware(userStore *store.UserStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(UserIDKey).(string)
			if !ok {
				types.WriteError(w, types.ErrUnauthorized)
				return
			}

			user, err := userStore.GetUserByID(r.Context(), userID)
			if err != nil || user == nil || !user.IsAdmin {
				types.WriteError(w, types.ErrNotFound) // Hide behind 404 for security
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
