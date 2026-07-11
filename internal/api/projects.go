package api

import (
	"encoding/json"
	"net/http"
	"log/slog"

	"forge/internal/auth"
	"forge/internal/config"
	"forge/internal/store"
	"forge/internal/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

import "forge/internal/events"

type ProjectsHandler struct {
	projectStore *store.ProjectStore
	userStore    *store.UserStore
	config       *config.Config
	eventBus     *events.EventBus
}

func NewProjectsHandler(ps *store.ProjectStore, us *store.UserStore, cfg *config.Config, eb *events.EventBus) *ProjectsHandler {
	return &ProjectsHandler{
		projectStore: ps,
		userStore:    us,
		config:       cfg,
		eventBus:     eb,
	}
}

func (h *ProjectsHandler) Register(r chi.Router, rateLimiters map[string]func(http.Handler) http.Handler) {
	r.Group(func(r chi.Router) {
		r.Use(auth.SessionMiddleware(nil, h.userStore)) // Will be passed real session store from router later
		// Actually, I'll let the main router setup the group with session middleware
	})
	// I'll wire these endpoints to expect UserIDKey in context
}

// Separate registration for projects
func (h *ProjectsHandler) MountRoutes(r chi.Router) {
	r.Get("/", h.listProjects)
	r.Post("/", h.createProject)

	r.Route("/{id}", func(r chi.Router) {
		r.Use(h.ownerMiddleware)
		r.Get("/", h.getProject)
		r.Patch("/", h.updateProject)
		r.Delete("/", h.deleteProject)
		r.Get("/stream", h.streamEvents)
		r.Post("/tasks/{taskId}/abort", h.abortTask)
		r.Post("/tasks", h.createTask)
	})
}

type CreateTaskRequest struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"`
}

func (h *ProjectsHandler) createTask(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		types.WriteError(w, types.ErrValidationFailed)
		return
	}

	taskID := uuid.New().String()

	startReq := map[string]interface{}{
		"op":         "project.start",
		"project_id": projectID,
	}

	// We assume a NATS connection is available via eventBus.nc
	err := h.eventBus.RequestRPC("worker-1", startReq, &map[string]interface{}{})
	if err != nil {
		slog.Error("failed to request start project", "error", err)
	}

	// Then send the prompt
	promptReq := map[string]interface{}{
		"op":         "agent.prompt",
		"project_id": projectID,
		"task_id":    taskID,
		"prompt":     req.Prompt,
	}
	err = h.eventBus.RequestRPC("worker-1", promptReq, &map[string]interface{}{})
	if err != nil {
		slog.Error("failed to request prompt", "error", err)
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": taskID, "status": "queued"})
}

func (h *ProjectsHandler) abortTask(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusAccepted)
}

func (h *ProjectsHandler) ownerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(auth.UserIDKey).(string)
		projectID := chi.URLParam(r, "id")

		// Let admin see all
		user, err := h.userStore.GetUserByID(r.Context(), userID)
		if err != nil || user == nil {
			types.WriteError(w, types.ErrUnauthorized)
			return
		}

		ownerID, err := h.projectStore.GetProjectOwner(r.Context(), projectID)
		if err != nil {
			types.WriteError(w, types.ErrNotFound)
			return
		}

		if ownerID == "" {
			types.WriteError(w, types.ErrNotFound)
			return
		}

		if ownerID != userID && !user.IsAdmin {
			types.WriteError(w, types.ErrNotFound) // Hide behind 404
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *ProjectsHandler) listProjects(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(auth.UserIDKey).(string)
	projects, err := h.projectStore.ListProjects(r.Context(), userID, 50)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.ProjectsResponse{Projects: projects})
}

func (h *ProjectsHandler) createProject(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(auth.UserIDKey).(string)

	var req types.CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		types.WriteError(w, types.ErrValidationFailed)
		return
	}

	count, err := h.projectStore.CountUserProjects(r.Context(), userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if count >= h.config.MaxProjectsPerUser {
		types.WriteError(w, types.ErrValidationFailed)
		return
	}

	previewID, err := auth.GeneratePreviewID()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	p, err := h.projectStore.CreateProject(r.Context(), userID, req.Name, req.Template, previewID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(p)
}

func (h *ProjectsHandler) getProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.projectStore.GetProjectFull(r.Context(), id, h.config.Domain)
	if err != nil || p == nil {
		types.WriteError(w, types.ErrNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

func (h *ProjectsHandler) updateProject(w http.ResponseWriter, r *http.Request) {
	// Stub for M1
	w.WriteHeader(http.StatusOK)
}

func (h *ProjectsHandler) deleteProject(w http.ResponseWriter, r *http.Request) {
	// Stub for M1 (just return 204)
	w.WriteHeader(http.StatusNoContent)
}
