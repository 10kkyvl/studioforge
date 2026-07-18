package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}
type Transport interface {
	ListTools(context.Context) ([]Tool, error)
	Call(context.Context, string, map[string]any) (json.RawMessage, error)
	Close() error
}
type Client struct {
	transport Transport
	mu        sync.RWMutex
	tools     map[string]Tool
}

func NewClient(t Transport) *Client { return &Client{transport: t, tools: map[string]Tool{}} }
func (c *Client) Discover(ctx context.Context) ([]Tool, error) {
	tools, err := c.transport.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("Studio MCP capability discovery: %w", err)
	}
	c.mu.Lock()
	c.tools = map[string]Tool{}
	for _, tool := range tools {
		c.tools[tool.Name] = tool
	}
	c.mu.Unlock()
	return tools, nil
}
func (c *Client) Has(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.tools[name]
	return ok
}

// Call invokes a tool without first consulting the advertised tool list.
//
// The list cannot be trusted as a gate: the Studio plugin pushes it only to
// whichever launcher won the WS host port, so a second MCP client on the same
// machine is advertised zero tools permanently while every call it makes still
// succeeds through the host. Gating here would deny working calls whenever the
// operator has another MCP client attached to Studio. A tool that genuinely
// does not exist reports itself as a method-not-found error instead.
func (c *Client) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	return c.transport.Call(ctx, name, args)
}

// Instance is a Studio window reported by the launcher.
type Instance struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// ListStudios reports the Studio instances currently reachable through the
// launcher.
func (c *Client) ListStudios(ctx context.Context) ([]Instance, error) {
	raw, err := c.Call(ctx, "list_roblox_studios", nil)
	if err != nil {
		return nil, err
	}
	text, err := TextResult(raw)
	if err != nil {
		return nil, err
	}
	var listing struct {
		Studios []Instance `json:"studios"`
	}
	if err := json.Unmarshal([]byte(text), &listing); err != nil {
		return nil, fmt.Errorf("decode Studio instance list %q: %w", text, err)
	}
	return listing.Studios, nil
}

// TextResult unwraps the single text payload of an MCP tool result.
func TextResult(raw json.RawMessage) (string, error) {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("decode Studio MCP tool result: %w", err)
	}
	if result.IsError {
		return "", fmt.Errorf("Studio MCP tool returned an error: %s", raw)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		return "", fmt.Errorf("unexpected Studio MCP tool result: %s", raw)
	}
	return result.Content[0].Text, nil
}

func (c *Client) SelectStudio(ctx context.Context, instanceID string) error {
	if instanceID == "" {
		return errors.New("Studio instance ID is required; ambiguous Studio selection is refused")
	}
	_, err := c.Call(ctx, "set_active_studio", map[string]any{"studio_id": instanceID})
	return err
}
func (c *Client) Close() error { return c.transport.Close() }
