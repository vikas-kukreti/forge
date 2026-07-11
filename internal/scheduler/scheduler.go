package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"forge/internal/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

type Scheduler struct {
	pool  *pgxpool.Pool
	nc    *nats.Conn
	nodes map[string]types.NodeHeartbeat
	mu    sync.RWMutex
}

func NewScheduler(pool *pgxpool.Pool, nc *nats.Conn) *Scheduler {
	return &Scheduler{
		pool:  pool,
		nc:    nc,
		nodes: make(map[string]types.NodeHeartbeat),
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	// Subscribe to all node heartbeats
	sub, err := s.nc.Subscribe("forge.node.*.hb", func(msg *nats.Msg) {
		var hb types.NodeHeartbeat
		if err := json.Unmarshal(msg.Data, &hb); err != nil {
			slog.Error("failed to unmarshal heartbeat", "error", err)
			return
		}

		s.mu.Lock()
		s.nodes[hb.Name] = hb
		s.mu.Unlock()

		// Upsert node in db
		_, err := s.pool.Exec(context.Background(), `
			INSERT INTO nodes (name, internal_addr, cpu_millicores, mem_mb, disk_mb, alloc_cpu_millicores, alloc_mem_mb, alloc_disk_mb, agent_version, last_heartbeat)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
			ON CONFLICT (name) DO UPDATE SET
				internal_addr = EXCLUDED.internal_addr,
				cpu_millicores = EXCLUDED.cpu_millicores,
				mem_mb = EXCLUDED.mem_mb,
				disk_mb = EXCLUDED.disk_mb,
				alloc_cpu_millicores = EXCLUDED.alloc_cpu_millicores,
				alloc_mem_mb = EXCLUDED.alloc_mem_mb,
				alloc_disk_mb = EXCLUDED.alloc_disk_mb,
				agent_version = EXCLUDED.agent_version,
				last_heartbeat = EXCLUDED.last_heartbeat,
				status = 'ready'
		`, hb.Name, hb.InternalAddr, hb.Caps.CPUMillicores, hb.Caps.MemMB, hb.Caps.DiskMB,
			hb.Alloc.CPUMillicores, hb.Alloc.MemMB, hb.Alloc.DiskMB, hb.Version)
		if err != nil {
			slog.Error("failed to upsert node", "error", err)
		}
	})
	if err != nil {
		slog.Error("failed to subscribe to heartbeats", "error", err)
		return
	}

	go func() {
		<-ctx.Done()
		sub.Unsubscribe()
	}()
}

// SelectNode finds a node with enough capacity
func (s *Scheduler) SelectNode(reqMemMB, reqCPUMillicores, reqDiskMB int) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for name, hb := range s.nodes {
		if hb.Caps.MemMB-hb.Alloc.MemMB >= reqMemMB &&
			hb.Caps.CPUMillicores-hb.Alloc.CPUMillicores >= reqCPUMillicores &&
			hb.Caps.DiskMB-hb.Alloc.DiskMB >= reqDiskMB {
			return name, nil
		}
	}

	return "", errors.New("no available nodes")
}

// EnsureNodeActive checks if node has heartbeated recently
func (s *Scheduler) EnsureNodeActive(ctx context.Context, nodeName string) error {
	var lastHB time.Time
	err := s.pool.QueryRow(ctx, "SELECT last_heartbeat FROM nodes WHERE name = $1 AND status = 'ready'", nodeName).Scan(&lastHB)
	if err != nil {
		return err
	}
	if time.Since(lastHB) > 30*time.Second {
		return errors.New("node is down or uncommunicative")
	}
	return nil
}
