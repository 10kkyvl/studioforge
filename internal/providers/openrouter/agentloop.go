package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/10kkyvl/studioforge/internal/gitops"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/agenttools"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/mcpbridge"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/orclient"
)

const (
	defaultMaxTurns     = 24
	maxToolCalls        = 60
	maxRepeatToolCalls  = 3
	maxTotalOutputBytes = 512 * 1024
	maxTruncatedContent = 4096
)

type handle struct {
	events chan providers.Event
	done   chan struct{}
	cancel context.CancelFunc
	once   sync.Once
	mu     sync.Mutex
	result providers.Result
}

func (h *handle) Events() <-chan providers.Event { return h.events }
func (h *handle) Wait() providers.Result {
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.result
}
func (h *handle) Cancel() error { h.once.Do(h.cancel); return nil }

func (p *Provider) Start(ctx context.Context, req providers.RunRequest) (providers.RunHandle, error) {
	return p.run(ctx, req, nil)
}

func (p *Provider) Resume(ctx context.Context, req providers.ResumeRequest) (providers.RunHandle, error) {
	return p.run(ctx, req.RunRequest, nil)
}

func (p *Provider) run(parent context.Context, req providers.RunRequest, priorMessages []orclient.Message) (providers.RunHandle, error) {
	if req.RunID == "" {
		return nil, errors.New("run ID is required")
	}
	info, err := os.Stat(req.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("openrouter: working directory %q is not accessible: %w", req.WorkingDirectory, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("openrouter: working directory %q is not a directory", req.WorkingDirectory)
	}
	ctx, cancel := context.WithCancel(parent)
	h := &handle{events: make(chan providers.Event, 32), done: make(chan struct{}), cancel: cancel}
	p.mu.Lock()
	p.runs[req.RunID] = cancel
	p.mu.Unlock()
	go p.execute(ctx, req, priorMessages, h)
	return h, nil
}

type toolRouter struct {
	local  *agenttools.ToolSet
	studio *mcpbridge.Bridge
}

func (r *toolRouter) Definitions() []orclient.Tool {
	d := r.local.Definitions()
	if r.studio != nil {
		d = append(d, r.studio.Definitions()...)
	}
	return d
}

func (r *toolRouter) Has(name string) bool {
	return r.local.Has(name) || (r.studio != nil && r.studio.Has(name))
}

func (r *toolRouter) Execute(ctx context.Context, name string, args json.RawMessage) agenttools.Result {
	if r.local.Has(name) {
		return r.local.Execute(ctx, name, args)
	}
	if r.studio != nil && r.studio.Has(name) {
		return r.studio.Execute(ctx, name, args)
	}
	return agenttools.Result{IsError: true, Content: "unknown or unavailable tool: " + name}
}

func resolveProfile(raw string) agenttools.Profile {
	switch agenttools.Profile(raw) {
	case agenttools.ProfileReadOnly, agenttools.ProfileWorkspace, agenttools.ProfileDanger:
		return agenttools.Profile(raw)
	default:
		return agenttools.ProfileWorkspace
	}
}

func boolPtr(b bool) *bool { return &b }

func emit(ctx context.Context, h *handle, sessionID string, evt providers.Event) {
	evt.SessionID = sessionID
	evt.At = time.Now().UTC()
	select {
	case <-ctx.Done():
	case h.events <- evt:
	}
}

func finish(h *handle, result providers.Result) {
	h.mu.Lock()
	h.result = result
	h.mu.Unlock()
}

func persistTurn(ctx context.Context, convStore ConversationStore, threadID, runID string, assistantMsg StoredMessage, toolResults []orclient.Message) {
	persistMessage(ctx, convStore, threadID, runID, assistantMsg)
	for _, tm := range toolResults {
		content, _ := tm.Content.(string)
		persistMessage(ctx, convStore, threadID, runID, StoredMessage{Role: "tool", ToolCallID: tm.ToolCallID, Content: content})
	}
}

func (p *Provider) execute(ctx context.Context, req providers.RunRequest, priorMessages []orclient.Message, h *handle) {
	defer close(h.events)
	defer close(h.done)
	defer func() { p.mu.Lock(); delete(p.runs, req.RunID); p.mu.Unlock() }()
	defer h.Cancel()

	sessionID := "openrouter:" + req.RunID

	p.mu.Lock()
	keySource := p.keySource
	baseURL := p.baseURL
	p.mu.Unlock()

	key := ""
	if keySource != nil {
		key = keySource()
	}
	if key == "" {
		message := "OpenRouter API key is not configured"
		emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: "openrouter.auth", Payload: map[string]any{"message": message}, Error: message})
		finish(h, providers.Result{SessionID: sessionID, Err: errors.New(message), ExitCode: -1})
		return
	}

	client := orclient.New(orclient.Config{APIKey: key, BaseURL: baseURL})

	ws, err := agenttools.NewWorkspace(req.WorkingDirectory)
	if err != nil {
		emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: "openrouter.workspace", Payload: map[string]any{"message": err.Error()}, Error: err.Error()})
		finish(h, providers.Result{SessionID: sessionID, Err: err, ExitCode: -1})
		return
	}

	p.mu.Lock()
	modelInfoFn := p.modelInfo
	routing := p.routing
	p.mu.Unlock()

	var info ModelInfo
	known := false
	if modelInfoFn != nil {
		info, known = modelInfoFn(req.Model)
	}
	vision := known && info.Vision

	if len(req.Attachments) > 0 && !vision {
		message := "The selected model does not accept image input. Choose a vision-capable model (one marked with image support in the model list), or resend without the attachment."
		emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: "openrouter.image_unsupported", Payload: map[string]any{"message": message}, Error: message})
		finish(h, providers.Result{SessionID: sessionID, Err: errors.New(message), ExitCode: -1})
		return
	}

	toolset, err := agenttools.NewToolSet(resolveProfile(req.PermissionProfile), agenttools.Options{
		Workspace:  ws,
		Git:        gitops.New(),
		Supervisor: p.sup,
		ProjectID:  req.ProjectID,
		RunID:      req.RunID,
	})
	if err != nil {
		emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: "openrouter.tools", Payload: map[string]any{"message": err.Error()}, Error: err.Error()})
		finish(h, providers.Result{SessionID: sessionID, Err: err, ExitCode: -1})
		return
	}

	router := &toolRouter{local: toolset}
	studioContext := ""
	p.mu.Lock()
	connector := p.connector
	convStore := p.store
	p.mu.Unlock()
	if connector != nil {
		grant := connector(ctx, req.ProjectID, req.RunID, req.PermissionProfile)
		if grant.Notice != "" {
			emit(ctx, h, sessionID, providers.Event{Type: "status", RawType: "openrouter.studio", Payload: map[string]any{"message": grant.Notice}})
		}
		if grant.Client != nil {
			router.studio = mcpbridge.New(ctx, grant.Client, grant.AllowedTools, 0)
			if grant.Release != nil {
				defer grant.Release()
			}
			studioContext = grant.Context
		}
	}

	threadID := req.ThreadID
	persist := convStore != nil && threadID != ""

	var messages []orclient.Message
	if len(priorMessages) > 0 {
		messages = append(messages, priorMessages...)
	} else {
		if req.SystemPrompt != "" {
			messages = append(messages, orclient.Message{Role: "system", Content: req.SystemPrompt})
		}
		if studioContext != "" {
			messages = append(messages, orclient.Message{Role: "system", Content: "Current Roblox Studio state:\n" + studioContext})
		}
		if persist {
			prior, _ := convStore.Load(ctx, threadID)
			messages = append(messages, sanitizeHistory(storedToMessages(prior, ws, vision))...)
		}
		userMsg, err := buildUserMessage(ws, req.Prompt, req.Attachments, vision)
		if err != nil {
			emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: "openrouter.attachment", Payload: map[string]any{"message": err.Error()}, Error: err.Error()})
			finish(h, providers.Result{SessionID: sessionID, Err: err, ExitCode: -1})
			return
		}
		messages = append(messages, userMsg)
	}
	if persist {
		persistMessage(ctx, convStore, threadID, req.RunID, StoredMessage{Role: "user", Content: req.Prompt, Attachments: req.Attachments})
	}

	maxTurns := req.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}

	var usage providers.Usage
	var cost float64
	toolCallCount := 0
	totalOutput := 0
	repeatCounts := map[string]int{}
	forceFinal := false
	compactionNotified := false

	for turn := 1; ; turn++ {
		final := forceFinal
		switch {
		case turn > maxTurns:
			final = true
			emit(ctx, h, sessionID, providers.Event{Type: "status", RawType: "openrouter.max_turns", Payload: map[string]any{"message": "maximum turns reached; requesting a final answer"}})
		case req.MaxBudget > 0 && cost >= req.MaxBudget:
			final = true
			emit(ctx, h, sessionID, providers.Event{Type: "status", RawType: "openrouter.budget", Payload: map[string]any{"message": "budget ceiling reached; requesting a final answer"}})
		case forceFinal:
			emit(ctx, h, sessionID, providers.Event{Type: "status", RawType: "openrouter.force_final", Payload: map[string]any{"message": "forcing a final answer"}})
		}

		var toolChoice any
		if final {
			toolChoice = "none"
		}

		sink := func(d orclient.Delta) {
			if d.Text == "" {
				return
			}
			emit(ctx, h, sessionID, providers.Event{Type: "message", RawType: "openrouter.message.partial", Payload: map[string]any{"text": d.Text}})
		}

		requestMessages, didCompact := compactMessages(messages, maxHistoryChars)
		if didCompact && !compactionNotified {
			compactionNotified = true
			emit(ctx, h, sessionID, providers.Event{Type: "status", RawType: "openrouter.compacted", Payload: map[string]any{"message": "Earlier conversation history was compacted to fit the context window."}})
		}

		providerPrefs := &orclient.ProviderPreferences{RequireParameters: boolPtr(true)}
		if routing.AllowFallbacks != nil {
			providerPrefs.AllowFallbacks = routing.AllowFallbacks
		}
		if routing.DataCollection != "" {
			providerPrefs.DataCollection = routing.DataCollection
		}
		if routing.ZDR {
			providerPrefs.ZDR = boolPtr(true)
		}
		if len(routing.Order) > 0 {
			providerPrefs.Order = routing.Order
		}

		chatReq := orclient.ChatRequest{
			Model:      req.Model,
			Messages:   requestMessages,
			Tools:      router.Definitions(),
			ToolChoice: toolChoice,
			Provider:   providerPrefs,
		}

		completion, err := client.StreamChat(ctx, chatReq, sink)
		if err != nil {
			if ctx.Err() != nil {
				finish(h, providers.Result{SessionID: sessionID, Usage: usage, Cost: cost, Err: ctx.Err(), ExitCode: -1})
				return
			}
			message := err.Error()
			hint := ""
			var apiErr *orclient.APIError
			if errors.As(err, &apiErr) {
				message = apiErr.Message
				hint = orclient.Action(apiErr.Kind)
			}
			emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: "openrouter.error", Payload: map[string]any{"message": message, "hint": hint}, Error: message})
			finish(h, providers.Result{SessionID: sessionID, Usage: usage, Cost: cost, Err: err, ExitCode: -1})
			return
		}

		cachedTokens := 0
		if completion.Usage.PromptTokensDetails != nil {
			cachedTokens = completion.Usage.PromptTokensDetails.CachedTokens
		}
		reasoningTokens := 0
		if completion.Usage.CompletionTokensDetails != nil {
			reasoningTokens = completion.Usage.CompletionTokensDetails.ReasoningTokens
		}
		turnUsage := providers.Usage{
			InputTokens:     completion.Usage.PromptTokens - cachedTokens,
			OutputTokens:    completion.Usage.CompletionTokens,
			CacheReadTokens: cachedTokens,
		}
		usage = usage.Add(turnUsage)

		estimated := false
		switch {
		case completion.Usage.Cost > 0:
			cost += completion.Usage.Cost
		case turnUsage.InputTokens+turnUsage.OutputTokens > 0 && known && (info.PromptPrice > 0 || info.CompletionPrice > 0):
			cost += info.PromptPrice*float64(turnUsage.InputTokens) + info.CompletionPrice*float64(turnUsage.OutputTokens)
			estimated = true
		default:
			cost += completion.Usage.Cost
		}

		emit(ctx, h, sessionID, providers.Event{
			Type: "usage", RawType: "openrouter.usage",
			Payload: map[string]any{
				"inputTokens": usage.InputTokens, "outputTokens": usage.OutputTokens, "cacheReadTokens": usage.CacheReadTokens,
				"reasoningTokens": reasoningTokens, "cost": cost, "estimated": estimated,
			},
			Usage: usage, Cost: cost,
		})

		if completion.Content != "" {
			emit(ctx, h, sessionID, providers.Event{Type: "message", RawType: "openrouter.message", Payload: map[string]any{"text": completion.Content}})
		}

		if len(completion.ToolCalls) == 0 || final {
			if persist {
				persistMessage(ctx, convStore, threadID, req.RunID, StoredMessage{Role: "assistant", Content: completion.Content, Model: req.Model, Usage: turnUsage})
			}
			finish(h, providers.Result{SessionID: sessionID, Cost: cost, Usage: usage, ExitCode: 0})
			return
		}

		messages = append(messages, orclient.Message{Role: "assistant", Content: completion.Content, ToolCalls: completion.ToolCalls})

		var toolCallsJSON string
		if persist {
			b, _ := json.Marshal(completion.ToolCalls)
			toolCallsJSON = string(b)
		}
		assistantMsg := StoredMessage{Role: "assistant", Content: completion.Content, ToolCallsJSON: toolCallsJSON, Model: req.Model, Usage: turnUsage}

		var turnToolResults []orclient.Message

		for _, tc := range completion.ToolCalls {
			toolCallCount++

			emit(ctx, h, sessionID, providers.Event{Type: "tool", RawType: "tool.call", Payload: map[string]any{"tool": tc.Function.Name, "arguments": tc.Function.Arguments}})

			signature := tc.Function.Name + "\x00" + tc.Function.Arguments
			repeatCounts[signature]++

			var content string
			isError := false
			switch {
			case toolCallCount > maxToolCalls:
				content = "tool call budget exceeded for this run; no further tools will be executed"
				isError = true
				forceFinal = true
			case repeatCounts[signature] > maxRepeatToolCalls:
				content = "This exact tool call was repeated too many times. Stop repeating it; try a different approach or give your final answer."
				isError = true
				forceFinal = true
			case !router.Has(tc.Function.Name):
				content = "unknown or unavailable tool: " + tc.Function.Name
				isError = true
			default:
				res := router.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
				content = res.Content
				isError = res.IsError
				if content == "" {
					content = "(no output)"
				}
				totalOutput += len(content)
				if totalOutput > maxTotalOutputBytes {
					if len(content) > maxTruncatedContent {
						content = content[:maxTruncatedContent]
					}
					content += "\n... (truncated: run tool-output budget exceeded)"
					forceFinal = true
				}
			}

			emit(ctx, h, sessionID, providers.Event{Type: "tool", RawType: "tool.result", Payload: map[string]any{"tool": tc.Function.Name, "result": content, "isError": isError}})

			messages = append(messages, orclient.Message{Role: "tool", ToolCallID: tc.ID, Content: content})
			turnToolResults = append(turnToolResults, orclient.Message{Role: "tool", ToolCallID: tc.ID, Content: content})

			if ctx.Err() != nil {
				if persist {
					persistTurn(context.WithoutCancel(ctx), convStore, threadID, req.RunID, assistantMsg, turnToolResults)
				}
				finish(h, providers.Result{SessionID: sessionID, Usage: usage, Cost: cost, Err: ctx.Err(), ExitCode: -1})
				return
			}
		}

		if persist {
			persistTurn(ctx, convStore, threadID, req.RunID, assistantMsg, turnToolResults)
		}
	}
}
