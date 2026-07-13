package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"sync"

	"github.com/gorilla/websocket"
	"github.com/go-chi/chi/v5"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all for now
	},
}

func (h *ProjectsHandler) streamEvents(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("failed to upgrade websocket", "error", err)
		return
	}
	defer conn.Close()

	// Initial hello frame
	hello := map[string]interface{}{
		"type": "hello",
		"data": map[string]interface{}{
			"last_seq":             0,
			"runtime_state":        "running",
			"balance_microcredits": 50000000,
		},
	}
	if err := conn.WriteJSON(hello); err != nil {
		return
	}

	var mu sync.Mutex

	// Ping-pong to keep alive and read loop
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var p map[string]interface{}
			if err := json.Unmarshal(msg, &p); err == nil && p["type"] == "ping" {
				mu.Lock()
				conn.WriteJSON(map[string]interface{}{"type": "pong"})
				mu.Unlock()
			}
		}
	}()

	projectID := chi.URLParam(r, "id")
	ch := h.eventBus.Subscribe(projectID)
	defer h.eventBus.Unsubscribe(projectID, ch)

	// Stream live events from NATS fan-out
	for {
		select {
		case evBytes, ok := <-ch:
			if !ok {
				return
			}
			mu.Lock()
			err := conn.WriteMessage(websocket.TextMessage, evBytes)
			mu.Unlock()
			if err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
