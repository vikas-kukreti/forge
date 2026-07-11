package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"forge/internal/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

type EventBus struct {
	pool        *pgxpool.Pool
	nc          *nats.Conn
	subscribers map[string]map[chan []byte]bool
	mu          sync.RWMutex
}

func NewEventBus(pool *pgxpool.Pool, nc *nats.Conn) *EventBus {
	return &EventBus{
		pool:        pool,
		nc:          nc,
		subscribers: make(map[string]map[chan []byte]bool),
	}
}

func (eb *EventBus) Start(ctx context.Context) {
	// Persist events (queue group)
	eb.nc.QueueSubscribe("forge.proj.*.events", "persist", func(msg *nats.Msg) {
		tokens := strings.Split(msg.Subject, ".")
		if len(tokens) < 4 {
			return
		}
		projectID := tokens[2]

		var env types.EventEnvelope
		if err := json.Unmarshal(msg.Data, &env); err != nil {
			slog.Error("failed to unmarshal event", "error", err)
			return
		}

		payloadData, _ := json.Marshal(env.Data)

		_, err := eb.pool.Exec(context.Background(), `
			INSERT INTO agent_events (project_id, seq, task_id, type, payload, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT DO NOTHING
		`, projectID, env.Seq, env.TaskID, env.Type, payloadData, env.Ts)
		if err != nil {
			slog.Error("failed to persist event", "error", err)
		}
	})

	// Fan out to WebSockets
	eb.nc.Subscribe("forge.proj.*.events", func(msg *nats.Msg) {
		tokens := strings.Split(msg.Subject, ".")
		if len(tokens) < 4 {
			return
		}
		projectID := tokens[2]

		eb.mu.RLock()
		subs, ok := eb.subscribers[projectID]
		if ok {
			for ch := range subs {
				// non-blocking send
				select {
				case ch <- msg.Data:
				default:
				}
			}
		}
		eb.mu.RUnlock()
	})
}

func (eb *EventBus) RequestRPC(nodeName string, req interface{}, resp interface{}) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	msg, err := eb.nc.Request("forge.node."+nodeName+".rpc", b, 10000000000) // 10s
	if err != nil {
		return err
	}
	return json.Unmarshal(msg.Data, resp)
}

func (eb *EventBus) Subscribe(projectID string) chan []byte {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan []byte, 100)
	if eb.subscribers[projectID] == nil {
		eb.subscribers[projectID] = make(map[chan []byte]bool)
	}
	eb.subscribers[projectID][ch] = true
	return ch
}

func (eb *EventBus) Unsubscribe(projectID string, ch chan []byte) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if subs, ok := eb.subscribers[projectID]; ok {
		delete(subs, ch)
		if len(subs) == 0 {
			delete(eb.subscribers, projectID)
		}
	}
	close(ch)
}
