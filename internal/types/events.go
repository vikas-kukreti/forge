package types

import "time"

type EventEnvelope struct {
	Seq       int64                  `json:"seq"`
	Ts        time.Time              `json:"ts"`
	TaskID    *string                `json:"task_id"`
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data"`
}
