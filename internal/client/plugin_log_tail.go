package client

import (
	"context"
	"errors"
	"io"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

// PluginLogTail reads a finite, host-redacted diagnostic snapshot. The host
// retains bounded history while the frontend is attached to a raw terminal.
// This helper never follows logs or owns a worker beyond its caller's request.
func (c *Client) PluginLogTail(ctx context.Context, target *apb.PluginTarget, plugin string, limit uint32) ([]*pb.LogRecord, error) {
	if limit == 0 || limit > 20 {
		return nil, errors.New("diagnostic log tail must contain 1–20 records")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	stream, err := c.Agent().WatchPluginLogs(ctx, &apb.PluginLogsRequest{Target: target, Request: &pb.PluginLogsRequest{PluginId: plugin, Tail: limit, Follow: false}})
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	var records []*pb.LogRecord
	for uint32(len(records)) < limit {
		record, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			return records, nil
		}
		if err != nil {
			return records, err
		}
		records = append(records, record)
	}
	return records, nil
}
