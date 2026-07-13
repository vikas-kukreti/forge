package natsutil

import (
	"encoding/json"

	"github.com/nats-io/nats.go"
)

func PublishJSON(nc *nats.Conn, subject string, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return nc.Publish(subject, b)
}

func RequestJSON(nc *nats.Conn, subject string, req interface{}, resp interface{}) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	msg, err := nc.Request(subject, b, 5000000000) // 5 seconds
	if err != nil {
		return err
	}
	return json.Unmarshal(msg.Data, resp)
}
