package mcpbridge

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/10kkyvl/studioforge/internal/providers/openrouter/agenttools"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/orclient"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
)

const (
	defaultMaxResultBytes = 32 * 1024
	mcpCallTimeout        = 60 * time.Second
)

type Bridge struct {
	client         *mcp.Client
	allowed        map[string]bool
	advertised     []mcp.Tool
	maxResultBytes int
}

func New(ctx context.Context, client *mcp.Client, allowedPrefixed []string, maxResultBytes int) *Bridge {
	if maxResultBytes <= 0 {
		maxResultBytes = defaultMaxResultBytes
	}
	allowed := map[string]bool{}
	for _, name := range allowedPrefixed {
		allowed[strings.TrimPrefix(name, mcp.ToolPrefix)] = true
	}
	discovered, _ := client.Discover(ctx)
	var advertised []mcp.Tool
	for _, tool := range discovered {
		if allowed[tool.Name] {
			advertised = append(advertised, tool)
		}
	}
	return &Bridge{client: client, allowed: allowed, advertised: advertised, maxResultBytes: maxResultBytes}
}

func (b *Bridge) Definitions() []orclient.Tool {
	defs := make([]orclient.Tool, 0, len(b.advertised))
	for _, tool := range b.advertised {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			schema = json.RawMessage(`{}`)
		}
		defs = append(defs, orclient.Tool{
			Type: "function",
			Function: orclient.ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  schema,
			},
		})
	}
	return defs
}

func (b *Bridge) Names() []string {
	out := make([]string, 0, len(b.advertised))
	for _, tool := range b.advertised {
		out = append(out, tool.Name)
	}
	return out
}

func (b *Bridge) Has(name string) bool {
	for _, tool := range b.advertised {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func (b *Bridge) Execute(ctx context.Context, name string, args json.RawMessage) agenttools.Result {
	if !b.allowed[name] {
		return agenttools.Result{IsError: true, Content: "Studio tool not permitted for this permission profile: " + name}
	}
	var argsMap map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argsMap); err != nil {
			return agenttools.Result{IsError: true, Content: "invalid Studio tool arguments: " + err.Error()}
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()
	raw, err := b.client.Call(callCtx, name, argsMap)
	if err != nil {
		if mcp.IsMethodNotFound(err) {
			return agenttools.Result{IsError: true, Content: "Studio tool is not available in this Studio: " + name}
		}
		return agenttools.Result{IsError: true, Content: "Studio tool call failed: " + err.Error()}
	}
	text, err := mcp.TextResult(raw)
	if err != nil {
		return agenttools.Result{IsError: true, Content: truncate(err.Error(), b.maxResultBytes)}
	}
	return agenttools.Result{Content: truncate(text, b.maxResultBytes)}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := strings.ToValidUTF8(s[:max], "")
	return cut + "\n... (truncated: Studio tool result exceeded the output budget)"
}
