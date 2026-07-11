package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"forge/internal/credits"
	"forge/internal/store"
	"forge/internal/types"
	"github.com/go-chi/chi/v5"
)

type AdminHandler struct {
	userStore *store.UserStore
	credMgr   *credits.Manager
}

func NewAdminHandler(us *store.UserStore, cm *credits.Manager) *AdminHandler {
	return &AdminHandler{
		userStore: us,
		credMgr:   cm,
	}
}

func (h *AdminHandler) MountRoutes(r chi.Router) {
	r.Get("/users", h.listUsers)
	r.Post("/users/{id}/grant", h.grantCredits)
	r.Post("/users/{id}/suspend", h.suspendUser)
}

func (h *AdminHandler) listUsers(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	users, err := h.userStore.ListUsers(r.Context(), limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"users": users})
}

func (h *AdminHandler) grantCredits(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")

	var req types.AdminGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		types.WriteError(w, types.ErrValidationFailed)
		return
	}

	// Microcredits
	delta := req.Credits * 1000000

	err := h.credMgr.GrantCredits(r.Context(), userID, delta, "admin_grant")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) suspendUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")

	err := h.userStore.SuspendUser(r.Context(), userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
