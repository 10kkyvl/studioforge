package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/10kkyvl/studioforge/internal/studiochanges"
)

type journalTransport struct {
	Transport
	recorder studiochanges.Recorder
}

func (t *journalTransport) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	if !studiochanges.Mutating(name) {
		return t.Transport.Call(ctx, name, args)
	}
	id, err := t.recorder.Start(ctx, name, args)
	if err != nil {
		return nil, fmt.Errorf("Studio change journal unavailable; tool was not dispatched: %w", err)
	}
	raw, callErr := t.Transport.Call(ctx, name, args)
	status := "succeeded"
	var result *struct {
		IsError bool `json:"isError"`
	}
	if callErr != nil || json.Unmarshal(raw, &result) != nil || result == nil {
		status = "unknown"
	} else if result.IsError {
		status = "error"
	}
	// A cancelled run still needs the outcome saved; a lost connection does not
	// prove the mutation did not happen. Leave an explicit unknown status.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := t.recorder.Finish(finishCtx, id, status); err != nil {
		return nil, fmt.Errorf("Studio tool was dispatched but its journal outcome could not be saved; inspect Studio before retrying: %w", err)
	}
	return raw, callErr
}

// SetRecorder is called before handing a per-run client to its agent loop.
func (c *Client) SetRecorder(recorder studiochanges.Recorder) {
	if recorder != nil {
		c.transport = &journalTransport{Transport: c.transport, recorder: recorder}
	}
}
