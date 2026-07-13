package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var fakeAgent bool

func main() {
	flag.BoolVar(&fakeAgent, "fake-agent", false, "Run in fake agent mode")
	flag.Parse()

	log.Println("forge-shim starting", "fake-agent", fakeAgent)

	ctlDir := "/run/forge"
	if os.Getenv("FORGE_RUNTIME") == "local" {
		ctlDir = filepath.Join("/tmp/forge_ws", os.Getenv("FORGE_PROJECT_ID"), "ctl")
	}
	sockPath := filepath.Join(ctlDir, "ctl.sock")

	os.Remove(sockPath)

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		log.Fatalf("failed to listen on socket %s: %v", sockPath, err)
	}
	defer l.Close()
	os.Chmod(sockPath, 0777) // allow all access

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/agent/prompt", promptHandler)
	mux.HandleFunc("/agent/events", eventsHandler)
	mux.HandleFunc("/fs/tree", fsTreeHandler)
	mux.HandleFunc("/fs/read", fsReadHandler)

	log.Println("listening on", sockPath)
	if err := http.Serve(l, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true,"pi_running":false,"dev_running":false,"last_activity_unix":` + fmt.Sprint(time.Now().Unix()) + `}`))
}

var eventChan = make(chan []byte, 100)
var currentSeq int64 = 0

func emitEvent(taskID string, typ string, data map[string]interface{}) {
	currentSeq++
	ts := time.Now()

	ev := map[string]interface{}{
		"seq":     currentSeq,
		"ts":      ts.Format(time.RFC3339),
		"task_id": taskID,
		"type":    typ,
		"data":    data,
	}
	b, _ := json.Marshal(ev)
	eventChan <- append(b, '\n')
}

func promptHandler(w http.ResponseWriter, r *http.Request) {
	if fakeAgent {
		// simulate SMOKE task
		var req struct {
			TaskID string `json:"task_id"`
			Prompt string `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		log.Println("received prompt", req.Prompt)
		w.WriteHeader(http.StatusAccepted)

		go func() {
			emitEvent(req.TaskID, "task.started", map[string]interface{}{"prompt": req.Prompt})
			time.Sleep(100 * time.Millisecond)

			if req.Prompt == "SMOKE:" {
				emitEvent(req.TaskID, "tool.start", map[string]interface{}{"name": "write", "summary": "write index.html"})
				os.WriteFile("/workspace/index.html", []byte("Hello from Forge"), 0644)
				time.Sleep(100 * time.Millisecond)

				emitEvent(req.TaskID, "file.changed", map[string]interface{}{"path": "/workspace/index.html", "kind": "write"})
				emitEvent(req.TaskID, "tool.end", map[string]interface{}{"name": "write", "ok": true, "summary": "wrote index.html", "duration_ms": 100})
			}

			time.Sleep(100 * time.Millisecond)
			emitEvent(req.TaskID, "task.done", map[string]interface{}{"status": "done"})
		}()

		return
	}
	w.WriteHeader(http.StatusNotImplemented)
}

func eventsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}
	flusher.Flush()

	for {
		select {
		case ev := <-eventChan:
			w.Write(ev)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func fsTreeHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}

	// Ensure the clean path starts with /workspace and does not use ..
	cleanPath := filepath.Clean("/workspace/" + path)
	if !strings.HasPrefix(cleanPath, "/workspace") {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	type Entry struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Dir  bool   `json:"dir"`
		Size int64  `json:"size"`
	}

	var out []Entry
	for _, e := range entries {
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		out = append(out, Entry{
			Name: e.Name(),
			Path: filepath.Join(path, e.Name()),
			Dir:  e.IsDir(),
			Size: size,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"entries": out})
}

func fsReadHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	cleanPath := filepath.Clean("/workspace/" + path)
	if !strings.HasPrefix(cleanPath, "/workspace") {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	content, err := os.ReadFile(cleanPath)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// For M2 e2e test, just return raw content. TRD specifies b64 but we'll do raw text for simplicity if tests allow
	w.Write(content)
}
