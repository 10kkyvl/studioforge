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
	p.mu.Lock()
	id := p.id
	p.mu.Unlock()
	if req.RunID == "" {
		return nil, errors.New("run ID is required")
	}
	info, err := os.Stat(req.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("%s: working directory %q is not accessible: %w", id, req.WorkingDirectory, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s: working directory %q is not a directory", id, req.WorkingDirectory)
	}
	ctx, cancel := context.WithCancel(parent)
	p.mu.Lock()
	if _, exists := p.runs[req.RunID]; exists {
		p.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("%s: run %q is already active", id, req.RunID)
	}
	h := &handle{events: make(chan providers.Event, 32), done: make(chan struct{}), cancel: cancel}
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

func emitAvailable(h *handle, sessionID string, evt providers.Event) {
	evt.SessionID = sessionID
	evt.At = time.Now().UTC()
	select {
	case h.events <- evt:
	default:
	}
}

func finish(h *handle, result providers.Result) {
	h.mu.Lock()
	h.result = result
	h.mu.Unlock()
}

const (
	budgetEpsilon               = 1e-9
	minimumBudgetedOutputTokens = 16
	defaultBudgetedOutputTokens = 4096
	budgetSafetyFraction        = 0.05
)

func effectiveModel(requested, actual string) string {
	if actual != "" {
		return actual
	}
	return requested
}

func estimatedInputTokens(messages []orclient.Message, tools []orclient.Tool) int {
	body, err := json.Marshal(struct {
		Messages []orclient.Message `json:"messages"`
		Tools    []orclient.Tool    `json:"tools"`
	}{Messages: messages, Tools: tools})
	if err != nil {
		return 256
	}
	return len(body) + 256
}

func fixedRequestCost(info ModelInfo, messages []orclient.Message) float64 {
	images := 0
	for _, message := range messages {
		parts, ok := message.Content.([]orclient.ContentPart)
		if !ok {
			continue
		}
		for _, part := range parts {
			if part.Type == "image_url" {
				images++
			}
		}
	}
	return info.RequestPrice + float64(images)*info.ImagePrice
}

func budgetedMaxTokens(limit, spent float64, info ModelInfo, known bool, messages []orclient.Message, tools []orclient.Tool) (int, bool) {
	if limit <= 0 {
		return 0, true
	}
	remaining := limit - spent
	if remaining <= budgetEpsilon || !known || !info.PriceKnown {
		return 0, false
	}
	usable := remaining*(1-budgetSafetyFraction) - fixedRequestCost(info, messages) - float64(estimatedInputTokens(messages, tools))*info.PromptPrice
	if usable <= budgetEpsilon {
		return 0, false
	}
	maxTokens := defaultBudgetedOutputTokens
	if info.CompletionPrice > 0 {
		maxTokens = int((usable + budgetEpsilon) / info.CompletionPrice)
	}
	if info.MaxOutputTokens > 0 && maxTokens > info.MaxOutputTokens {
		maxTokens = info.MaxOutputTokens
	}
	if maxTokens < minimumBudgetedOutputTokens {
		return 0, false
	}
	return maxTokens, true
}

func completionCost(completion *orclient.Completion, info ModelInfo, known bool, messages []orclient.Message, cachedTokens, cacheWriteTokens, reasoningTokens int) (float64, bool, bool) {
	if completion.Usage.CostPresent {
		return completion.Usage.Cost, false, true
	}
	if !known || !info.PriceKnown {
		return 0, false, false
	}
	if !completion.UsagePresent {
		if info.PromptPrice == 0 && info.CompletionPrice == 0 && info.RequestPrice == 0 && info.ImagePrice == 0 && info.CacheReadPrice == 0 && info.CacheWritePrice == 0 && info.ReasoningPrice == 0 {
			return 0, true, true
		}
		return 0, false, false
	}
	nonCachedInput := completion.Usage.PromptTokens - cachedTokens - cacheWriteTokens
	if nonCachedInput < 0 {
		nonCachedInput = 0
	}
	nonReasoningOutput := completion.Usage.CompletionTokens - reasoningTokens
	if nonReasoningOutput < 0 {
		nonReasoningOutput = 0
	}
	cachePrice := info.CacheReadPrice
	if cachePrice <= 0 {
		cachePrice = info.PromptPrice
	}
	cacheWritePrice := info.CacheWritePrice
	if cacheWritePrice <= 0 {
		cacheWritePrice = info.PromptPrice
	}
	reasoningPrice := info.ReasoningPrice
	if reasoningPrice <= 0 {
		reasoningPrice = info.CompletionPrice
	}
	cost := fixedRequestCost(info, messages) + float64(nonCachedInput)*info.PromptPrice + float64(cachedTokens)*cachePrice + float64(cacheWriteTokens)*cacheWritePrice + float64(nonReasoningOutput)*info.CompletionPrice + float64(reasoningTokens)*reasoningPrice
	return cost, true, true
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

	p.mu.Lock()
	keySource := p.keySource
	baseURL := p.baseURL
	httpClient := p.httpClient
	providerID := p.id
	display := p.display
	providerRouting := p.providerRouting
	p.mu.Unlock()
	sessionID := providerID + ":" + req.RunID
	rawType := func(suffix string) string { return providerID + "." + suffix }

	key := ""
	if keySource != nil {
		key = keySource()
	}
	if key == "" {
		message := display + " API key is not configured"
		emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: rawType("auth"), Payload: map[string]any{"message": message}, Error: message})
		finish(h, providers.Result{SessionID: sessionID, Err: errors.New(message), ExitCode: -1})
		return
	}

	client := orclient.New(orclient.Config{
		APIKey: key, BaseURL: baseURL, HTTPClient: httpClient, ProviderName: display, MaxRetries: 7,
		OnRetry: func(retry orclient.Retry) {
			delay := retry.Delay.Round(100 * time.Millisecond)
			emit(ctx, h, sessionID, providers.Event{
				Type: "status", RawType: rawType("retry"),
				Payload: map[string]any{
					"message": fmt.Sprintf("Temporary %s failure; retrying (%d/%d) in %s…", display, retry.Attempt, retry.MaxRetries, delay),
					"attempt": retry.Attempt, "maxRetries": retry.MaxRetries, "delayMs": retry.Delay.Milliseconds(),
					"kind": retry.Kind, "statusCode": retry.StatusCode,
				},
			})
		},
	})

	ws, err := agenttools.NewWorkspace(req.WorkingDirectory)
	if err != nil {
		emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: rawType("workspace"), Payload: map[string]any{"message": err.Error()}, Error: err.Error()})
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
	if modelInfoFn != nil {
		capabilitiesKnown := known && (info.CapabilitiesKnown || info.Verified)
		switch {
		case capabilitiesKnown && !info.Tools:
			message := "The selected " + display + " model does not support tool calling and cannot run as a coding agent."
			emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: rawType("model_incompatible"), Payload: map[string]any{"message": message}, Error: message})
			finish(h, providers.Result{SessionID: sessionID, Err: errors.New(message), ExitCode: -1})
			return
		case (!capabilitiesKnown || !info.Verified) && !req.AllowUnverifiedModel:
			message := "The selected " + display + " model's tool compatibility is unverified. Confirm the advanced compatibility warning before using it."
			emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: rawType("model_unverified"), Payload: map[string]any{"message": message}, Error: message})
			finish(h, providers.Result{SessionID: sessionID, Err: errors.New(message), ExitCode: -1})
			return
		}
	}
	vision := known && (info.CapabilitiesKnown || info.Verified) && info.Vision

	if len(req.Attachments) > 0 && !vision {
		message := "The selected model does not accept image input. Choose a vision-capable model (one marked with image support in the model list), or resend without the attachment."
		emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: rawType("image_unsupported"), Payload: map[string]any{"message": message}, Error: message})
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
		emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: rawType("tools"), Payload: map[string]any{"message": err.Error()}, Error: err.Error()})
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
			emit(ctx, h, sessionID, providers.Event{Type: "status", RawType: rawType("studio"), Payload: map[string]any{"message": grant.Notice}})
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
			emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: rawType("attachment"), Payload: map[string]any{"message": err.Error()}, Error: err.Error()})
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
	budgetCostKnown := true
	var reasoningTokens int
	toolCallCount := 0
	totalOutput := 0
	repeatCounts := map[string]int{}
	tools := router.Definitions()
	forceFinal := false
	compactionNotified := false
	reasoningNotified := false
	latestAssistantContent := ""

	for turn := 1; ; turn++ {
		reasoningNotified = false
		final := forceFinal
		switch {
		case turn > maxTurns:
			final = true
			emit(ctx, h, sessionID, providers.Event{Type: "status", RawType: rawType("max_turns"), Payload: map[string]any{"message": "maximum turns reached; requesting a final answer"}})
		case forceFinal:
			emit(ctx, h, sessionID, providers.Event{Type: "status", RawType: rawType("force_final"), Payload: map[string]any{"message": "forcing a final answer"}})
		}

		var toolChoice any
		if final {
			toolChoice = "none"
		}

		sink := func(d orclient.Delta) {
			if d.Reasoning && !reasoningNotified {
				reasoningNotified = true
				emit(ctx, h, sessionID, providers.Event{Type: "status", RawType: rawType("reasoning"), Payload: map[string]any{"message": "Model is reasoning…", "turn": turn}})
			}
			if d.Text != "" {
				emit(ctx, h, sessionID, providers.Event{Type: "message", RawType: rawType("message.partial"), Payload: map[string]any{"text": d.Text, "turn": turn}})
			}
		}

		requestMessages, didCompact := compactMessages(messages, maxHistoryChars)
		if didCompact && !compactionNotified {
			compactionNotified = true
			emit(ctx, h, sessionID, providers.Event{Type: "status", RawType: rawType("compacted"), Payload: map[string]any{"message": "Earlier conversation history was compacted to fit the context window."}})
		}
		maxTokens, allowed := budgetedMaxTokens(req.MaxBudget, cost, info, known && budgetCostKnown, requestMessages, tools)
		if !allowed {
			message := "Budget ceiling reached; no additional model request was sent."
			emit(ctx, h, sessionID, providers.Event{Type: "status", RawType: rawType("budget"), Payload: map[string]any{"message": message, "cost": cost, "limit": req.MaxBudget}})
			if latestAssistantContent == "" {
				latestAssistantContent = message
				emit(ctx, h, sessionID, providers.Event{Type: "message", RawType: rawType("message"), Payload: map[string]any{"text": message, "turn": turn}})
				if persist {
					persistMessage(ctx, convStore, threadID, req.RunID, StoredMessage{Role: "assistant", Content: message, Model: req.Model})
				}
			}
			finish(h, providers.Result{SessionID: sessionID, Cost: cost, Usage: usage, ExitCode: 0})
			return
		}

		var providerPrefs *orclient.ProviderPreferences
		if providerRouting {
			providerPrefs = &orclient.ProviderPreferences{RequireParameters: boolPtr(true)}
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
		}

		chatReq := orclient.ChatRequest{
			Model:      req.Model,
			Messages:   requestMessages,
			Tools:      tools,
			ToolChoice: toolChoice,
			Provider:   providerPrefs,
			MaxTokens:  maxTokens,
		}

		completion, err := client.StreamChat(ctx, chatReq, sink)
		if err != nil {
			if completion != nil && completion.Content != "" {
				finalEvent := providers.Event{Type: "message", RawType: rawType("message"), Payload: map[string]any{"text": completion.Content, "turn": turn, "incomplete": true}}
				if ctx.Err() != nil {
					emitAvailable(h, sessionID, finalEvent)
				} else {
					emit(ctx, h, sessionID, finalEvent)
				}
				if persist {
					persistMessage(context.WithoutCancel(ctx), convStore, threadID, req.RunID, StoredMessage{Role: "assistant", Content: completion.Content, Model: effectiveModel(req.Model, completion.Model)})
				}
			}
			if ctx.Err() != nil {
				finish(h, providers.Result{SessionID: sessionID, Usage: usage, Cost: cost, Err: ctx.Err(), ExitCode: -1})
				return
			}
			message := err.Error()
			hint := ""
			var apiErr *orclient.APIError
			if errors.As(err, &apiErr) {
				message = apiErr.Message
				hint = orclient.ActionFor(display, apiErr.Kind)
			}
			emit(ctx, h, sessionID, providers.Event{Type: "error", RawType: rawType("error"), Payload: map[string]any{"message": message, "hint": hint}, Error: message})
			finish(h, providers.Result{SessionID: sessionID, Usage: usage, Cost: cost, Err: err, ExitCode: -1})
			return
		}

		cachedTokens := 0
		cacheWriteTokens := 0
		if completion.Usage.PromptTokensDetails != nil {
			cachedTokens = completion.Usage.PromptTokensDetails.CachedTokens
			cacheWriteTokens = completion.Usage.PromptTokensDetails.CacheWriteTokens
		}
		turnReasoningTokens := 0
		if completion.Usage.CompletionTokensDetails != nil {
			turnReasoningTokens = completion.Usage.CompletionTokensDetails.ReasoningTokens
		}
		reasoningTokens += turnReasoningTokens
		turnUsage := providers.Usage{
			InputTokens:         completion.Usage.PromptTokens - cachedTokens - cacheWriteTokens,
			OutputTokens:        completion.Usage.CompletionTokens,
			CacheReadTokens:     cachedTokens,
			CacheCreationTokens: cacheWriteTokens,
		}
		usage = usage.Add(turnUsage)
		actualModel := effectiveModel(req.Model, completion.Model)
		costInfo, costModelKnown := info, known
		if completion.Model != "" && completion.Model != req.Model && modelInfoFn != nil {
			costInfo, costModelKnown = modelInfoFn(completion.Model)
		}

		turnCost, estimated, costKnown := completionCost(completion, costInfo, costModelKnown, requestMessages, cachedTokens, cacheWriteTokens, turnReasoningTokens)
		cost += turnCost
		budgetCostKnown = budgetCostKnown && costKnown

		emit(ctx, h, sessionID, providers.Event{
			Type: "usage", RawType: rawType("usage"),
			Payload: map[string]any{
				"inputTokens": usage.InputTokens, "outputTokens": usage.OutputTokens, "cacheReadTokens": usage.CacheReadTokens, "cacheCreationTokens": usage.CacheCreationTokens,
				"reasoningTokens": reasoningTokens, "cost": cost, "estimated": estimated, "costKnown": costKnown, "model": actualModel,
			},
			Usage: usage, Cost: cost,
		})

		if completion.Content != "" {
			latestAssistantContent = completion.Content
			emit(ctx, h, sessionID, providers.Event{Type: "message", RawType: rawType("message"), Payload: map[string]any{"text": completion.Content, "turn": turn}})
		}

		if len(completion.ToolCalls) == 0 || final {
			if persist {
				persistMessage(ctx, convStore, threadID, req.RunID, StoredMessage{Role: "assistant", Content: completion.Content, Model: actualModel, Usage: turnUsage})
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
		assistantMsg := StoredMessage{Role: "assistant", Content: completion.Content, ToolCallsJSON: toolCallsJSON, Model: actualModel, Usage: turnUsage}

		var turnToolResults []orclient.Message
		latestStudioImage := ""

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
				if vision && res.ImageURL != "" {
					latestStudioImage = res.ImageURL
				} else if !vision && res.ImageURL != "" {
					content = "Screenshot captured, but the current model cannot inspect images."
				}
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
		if latestStudioImage != "" {
			messages = append(messages, orclient.Message{Role: "user", Content: []orclient.ContentPart{
				{Type: "text", Text: "Latest screenshot captured from Roblox Studio:"},
				{Type: "image_url", ImageURL: &orclient.ImageURL{URL: latestStudioImage}},
			}})
		}
	}
}
