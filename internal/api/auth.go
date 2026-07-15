package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"forge/internal/auth"
	"forge/internal/config"
	"forge/internal/credits"
	"forge/internal/store"
	"forge/internal/types"
	"github.com/go-chi/chi/v5"
)

type AuthHandler struct {
	userStore    *store.UserStore
	sessionStore *store.SessionStore
	ledgerStore  *store.LedgerStore
	credMgr      *credits.Manager
	config       *config.Config
}

func NewAuthHandler(us *store.UserStore, ss *store.SessionStore, ls *store.LedgerStore, cm *credits.Manager, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		userStore:    us,
		sessionStore: ss,
		ledgerStore:  ls,
		credMgr:      cm,
		config:       cfg,
	}
}

func (h *AuthHandler) Register(r chi.Router, rateLimiters map[string]func(http.Handler) http.Handler) {
	r.With(rateLimiters["signup"]).Post("/auth/signup", h.signup)
	r.With(rateLimiters["login"]).Post("/auth/login", h.login)

	// Auth required endpoints
	r.Group(func(r chi.Router) {
		r.Use(auth.SessionMiddleware(h.sessionStore, h.userStore))
		r.Post("/auth/logout", h.logout)
		r.Get("/me", h.me)
		r.Get("/credits/ledger", h.ledger)
	})
}

func (h *AuthHandler) signup(w http.ResponseWriter, r *http.Request) {
	if h.config.Signups == "closed" {
		types.WriteError(w, types.ErrForbidden)
		return
	}

	var req types.SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		types.WriteError(w, types.ErrValidationFailed)
		return
	}

	if len(req.Password) < 10 {
		types.WriteError(w, types.ErrValidationFailed)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	isAdmin := false
	if h.config.AdminEmails != "" {
		for _, email := range strings.Split(h.config.AdminEmails, ",") {
			if strings.TrimSpace(email) == req.Email {
				isAdmin = true
				break
			}
		}
	}

	user, err := h.userStore.CreateUser(r.Context(), req.Email, hash, req.DisplayName, isAdmin)
	if err != nil {
		types.WriteError(w, types.ErrEmailTaken)
		return
	}

	// Grant initial credits
	if h.config.SignupGrantCredits > 0 {
		err = h.credMgr.GrantCredits(r.Context(), user.ID, h.config.SignupGrantCredits*1000000, "signup_grant")
		if err != nil {
			// Log error but continue
		}
		user.BalanceMicrocredits = h.config.SignupGrantCredits * 1000000
	}

	h.setSessionCookie(w, r, user.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(types.MeResponse{User: user})
}

func (h *AuthHandler) login(w http.ResponseWriter, r *http.Request) {
	var req types.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		types.WriteError(w, types.ErrValidationFailed)
		return
	}

	user, pwdHash, err := h.userStore.GetUserByEmail(r.Context(), req.Email)
	if err != nil || user == nil {
		types.WriteError(w, types.ErrInvalidCredentials)
		return
	}

	if user.Status == "suspended" {
		types.WriteError(w, types.ErrForbidden)
		return
	}

	if !auth.ComparePassword(req.Password, pwdHash) {
		types.WriteError(w, types.ErrInvalidCredentials)
		return
	}

	h.setSessionCookie(w, r, user.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.MeResponse{User: user})
}

func (h *AuthHandler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("forge_session")
	if err == nil {
		tokenHash := auth.HashToken(cookie.Value)
		h.sessionStore.DeleteSession(r.Context(), tokenHash)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "forge_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.config.TLS == "on",
		SameSite: http.SameSiteLaxMode,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) me(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(auth.UserIDKey).(string)
	user, err := h.userStore.GetUserByID(r.Context(), userID)
	if err != nil || user == nil {
		types.WriteError(w, types.ErrUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.MeResponse{User: user})
}

func (h *AuthHandler) ledger(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(auth.UserIDKey).(string)
	entries, err := h.ledgerStore.GetLedger(r.Context(), userID, 50, nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.LedgerResponse{Entries: entries})
}

func (h *AuthHandler) setSessionCookie(w http.ResponseWriter, r *http.Request, userID string) {
	token, tokenHash, _ := auth.GenerateToken()
	ip := ExtractIP(r)
	ua := r.Header.Get("User-Agent")
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	h.sessionStore.CreateSession(r.Context(), userID, tokenHash, &ip, &ua, expiresAt)

	http.SetCookie(w, &http.Cookie{
		Name:     "forge_session",
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.config.TLS == "on",
		SameSite: http.SameSiteLaxMode,
	})
}