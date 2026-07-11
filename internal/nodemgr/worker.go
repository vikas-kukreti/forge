package nodemgr

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"forge/internal/config"
	"forge/internal/types"
	"github.com/nats-io/nats.go"
)

type Worker struct {
	nc     *nats.Conn
	config *config.Config
}

func NewWorker(nc *nats.Conn, cfg *config.Config) *Worker {
	return &Worker{
		nc:     nc,
		config: cfg,
	}
}

func (w *Worker) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		// Send initial heartbeat immediately
		w.sendHeartbeat()

		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				w.sendHeartbeat()
			}
		}
	}()
}

func (w *Worker) sendHeartbeat() {
	// Dummy heartbeat for M2
	hb := types.NodeHeartbeat{
		Name:         w.config.NodeName,
		InternalAddr: w.config.NodeInternalAddr,
		Caps: types.NodeCaps{
			CPUMillicores: 4000,
			MemMB:         8192,
			DiskMB:        100000,
		},
		Alloc: types.NodeCaps{
			CPUMillicores: 0,
			MemMB:         0,
			DiskMB:        0,
		},
		Sandboxes: []types.SandboxInfo{},
		Apps:      []types.AppInfo{},
		Version:   "1.0",
	}

	data, err := json.Marshal(hb)
	if err != nil {
		slog.Error("failed to marshal heartbeat", "error", err)
		return
	}

	subject := "forge.node." + w.config.NodeName + ".hb" // In reality node_id is a UUID, but for registration we can use name or expect forged to map it
	if err := w.nc.Publish(subject, data); err != nil {
		slog.Error("failed to publish heartbeat", "error", err)
	}
}
