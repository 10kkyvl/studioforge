package orclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	APIKey       string
	BaseURL      string
	HTTPClient   *http.Client
	Referer      string
	Title        string
	UserAgent    string
	MaxRetries   int
	ProviderName string
	OnRetry      RetrySink
}

type Client struct {
	apiKey       string
	baseURL      string
	httpClient   *http.Client
	referer      string
	title        string
	userAgent    string
	maxRetries   int
	providerName string
	onRetry      RetrySink
}

func New(cfg Config) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	referer := cfg.Referer
	if referer == "" {
		referer = "https://github.com/10kkyvl/studioforge"
	}
	title := cfg.Title
	if title == "" {
		title = "StudioForge"
	}
	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = "StudioForge"
	}
	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}
	providerName := cfg.ProviderName
	if providerName == "" {
		providerName = "OpenRouter"
	}
	return &Client{
		apiKey:       cfg.APIKey,
		baseURL:      strings.TrimRight(baseURL, "/"),
		httpClient:   httpClient,
		referer:      referer,
		title:        title,
		userAgent:    userAgent,
		maxRetries:   maxRetries,
		providerName: providerName,
		onRetry:      cfg.OnRetry,
	}
}

func (c *Client) apiError(kind Kind, status int) *APIError {
	return &APIError{Kind: kind, StatusCode: status, Message: safeMessage(c.providerName, kind), Provider: c.providerName}
}

func (c *Client) setHeaders(r *http.Request) {
	r.Header.Set("Authorization", "Bearer "+c.apiKey)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("HTTP-Referer", c.referer)
	r.Header.Set("X-Title", c.title)
	r.Header.Set("User-Agent", c.userAgent)
	r.Header.Set("Accept", "text/event-stream")
}

func (c *Client) StreamChat(ctx context.Context, req ChatRequest, sink Sink) (*Completion, error) {
	req.Stream = true
	if req.StreamOptions == nil {
		req.StreamOptions = &StreamOptions{IncludeUsage: true}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, &APIError{Kind: KindMalformedResponse, Message: err.Error(), Provider: c.providerName}
	}

	url := c.baseURL + "/chat/completions"

	var last *Completion
	for attempt := 0; ; attempt++ {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if reqErr != nil {
			return nil, c.classifyTransportErr(ctx, reqErr)
		}
		c.setHeaders(httpReq)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			apiErr := c.classifyTransportErr(ctx, err)
			if !c.retry(ctx, attempt, apiErr, nil) {
				return last, apiErr
			}
			continue
		}

		if resp.StatusCode == http.StatusOK {
			completion, streamErr := c.readStream(ctx, resp, sink)
			resp.Body.Close()
			if streamErr == nil {
				return completion, nil
			}
			last = completion
			var apiErr *APIError
			if !errors.As(streamErr, &apiErr) || !c.retry(ctx, attempt, apiErr, nil) {
				return completion, streamErr
			}
			continue
		}

		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		apiErr := c.parseErrorBody(resp.StatusCode, errBody)
		apiErr.RetryAfter = parseRetryAfter(resp.Header)
		if c.retry(ctx, attempt, apiErr, resp.Header) {
			continue
		}
		return nil, apiErr
	}
}

func (c *Client) retry(ctx context.Context, attempt int, apiErr *APIError, header http.Header) bool {
	if apiErr == nil || attempt >= c.maxRetries || !retryable(apiErr) || ctx.Err() != nil {
		return false
	}
	wait := backoffDuration(attempt)
	if apiErr.RetryAfter > wait {
		wait = apiErr.RetryAfter
	}
	if header != nil && header.Get("Retry-After") != "" {
		if retryAfter := parseRetryAfter(header); retryAfter > wait {
			wait = retryAfter
		}
	}
	if c.onRetry != nil {
		c.onRetry(Retry{Attempt: attempt + 1, MaxRetries: c.maxRetries, Delay: wait, Kind: apiErr.Kind, StatusCode: apiErr.StatusCode})
	}
	return sleep(ctx, wait)
}

func retryable(err *APIError) bool {
	if err.retryable {
		return true
	}
	switch err.Kind {
	case KindNetworkFailure, KindRateLimited, KindModelUnavailable:
		return true
	case KindProviderFailure:
		return err.StatusCode == 0 || err.StatusCode == http.StatusRequestTimeout || err.StatusCode == http.StatusConflict || err.StatusCode == http.StatusTooEarly || err.StatusCode >= 500
	default:
		return false
	}
}

func sleep(ctx context.Context, wait time.Duration) bool {
	if wait <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func backoffDuration(attempt int) time.Duration {
	d := 500 * time.Millisecond
	for i := 0; i < attempt; i++ {
		d *= 2
	}
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	return d
}

func (c *Client) parseErrorBody(status int, body []byte) *APIError {
	message := strings.TrimSpace(string(body))
	var eb struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &eb); err == nil && eb.Error.Message != "" {
		message = eb.Error.Message
	}
	kind := classifyStatus(status, message)
	return c.apiError(kind, status)
}

func (c *Client) classifyTransportErr(ctx context.Context, err error) *APIError {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return c.apiError(KindCancelled, 0)
	}
	return c.apiError(KindNetworkFailure, 0)
}

func (c *Client) classifyStreamErr(ctx context.Context, err error) *APIError {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return c.apiError(KindCancelled, 0)
	}
	if errors.Is(err, bufio.ErrTooLong) {
		return c.apiError(KindMalformedResponse, 0)
	}
	return c.apiError(KindNetworkFailure, 0)
}

func (c *Client) readStream(ctx context.Context, resp *http.Response, sink Sink) (*Completion, error) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)

	result := &Completion{}
	toolByIndex := map[int]*ToolCall{}
	var toolOrder []int
	done := false

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			done = true
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return result, &APIError{Kind: KindMalformedResponse, Message: err.Error()}
		}

		if chunk.Model != "" {
			result.Model = chunk.Model
		}
		if chunk.Usage != nil {
			result.Usage = *chunk.Usage
			result.UsagePresent = true
		}

		choiceIndex := -1
		for i := range chunk.Choices {
			if chunk.Choices[i].Index == 0 {
				choiceIndex = i
				break
			}
		}
		hasChoice := choiceIndex >= 0
		if hasChoice {
			choice := chunk.Choices[choiceIndex]
			if choice.Delta.Content != "" {
				result.Content += choice.Delta.Content
				if sink != nil {
					sink(Delta{Text: choice.Delta.Content})
				}
			}
			if choice.Delta.Reasoning != "" && sink != nil {
				sink(Delta{Reasoning: true})
			}
			for _, tc := range choice.Delta.ToolCalls {
				t, ok := toolByIndex[tc.Index]
				if !ok {
					t = &ToolCall{}
					toolByIndex[tc.Index] = t
					toolOrder = append(toolOrder, tc.Index)
				}
				if tc.ID != "" {
					t.ID = tc.ID
				}
				if tc.Type != "" {
					t.Type = tc.Type
				}
				if tc.Function.Name != "" {
					t.Function.Name = tc.Function.Name
				}
				t.Function.Arguments += tc.Function.Arguments
			}
			if choice.FinishReason != "" {
				result.FinishReason = choice.FinishReason
			}
		}

		choiceError := hasChoice && chunk.Choices[choiceIndex].FinishReason == "error"
		if chunk.Error != nil || choiceError {
			result.ToolCalls = sortedToolCalls(toolByIndex, toolOrder)
			kind := KindProviderFailure
			status := 0
			if chunk.Error != nil {
				status = chunk.Error.Code
				kind = classifyStatus(status, chunk.Error.Message)
			}
			return result, c.apiError(kind, status)
		}
	}

	if err := scanner.Err(); err != nil {
		result.ToolCalls = sortedToolCalls(toolByIndex, toolOrder)
		return result, c.classifyStreamErr(ctx, err)
	}

	result.ToolCalls = sortedToolCalls(toolByIndex, toolOrder)
	if !done {
		// A valid SSE response that ends without [DONE] was truncated in
		// transit. Mark this specific malformed response as retryable; tools are
		// not executed until this method succeeds.
		err := c.apiError(KindMalformedResponse, 0)
		err.retryable = true
		return result, err
	}
	if result.UsagePresent && !validUsage(result.Usage) {
		return result, c.apiError(KindMalformedResponse, 0)
	}
	if result.FinishReason == "length" {
		return result, c.apiError(KindOutputLimit, 0)
	}
	if result.FinishReason != "stop" && result.FinishReason != "tool_calls" {
		return result, c.apiError(KindProviderFailure, 0)
	}
	if result.FinishReason == "tool_calls" && len(result.ToolCalls) == 0 {
		return result, c.apiError(KindMalformedResponse, 0)
	}
	if result.FinishReason == "stop" && len(result.ToolCalls) > 0 {
		return result, c.apiError(KindMalformedResponse, 0)
	}
	if result.Content == "" && len(result.ToolCalls) == 0 {
		return result, c.apiError(KindMalformedResponse, 0)
	}
	for _, call := range result.ToolCalls {
		if call.ID == "" || call.Function.Name == "" || !json.Valid([]byte(call.Function.Arguments)) {
			return result, c.apiError(KindMalformedResponse, 0)
		}
	}
	return result, nil
}

func validUsage(usage Usage) bool {
	if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.TotalTokens < 0 || usage.Cost < 0 || math.IsNaN(usage.Cost) || math.IsInf(usage.Cost, 0) {
		return false
	}
	if usage.PromptTokensDetails != nil {
		if usage.PromptTokensDetails.CachedTokens < 0 || usage.PromptTokensDetails.CacheWriteTokens < 0 || usage.PromptTokensDetails.CachedTokens+usage.PromptTokensDetails.CacheWriteTokens > usage.PromptTokens {
			return false
		}
	}
	if usage.CompletionTokensDetails != nil {
		if usage.CompletionTokensDetails.ReasoningTokens < 0 || usage.CompletionTokensDetails.ReasoningTokens > usage.CompletionTokens {
			return false
		}
	}
	return true
}

func sortedToolCalls(byIndex map[int]*ToolCall, order []int) []ToolCall {
	if len(order) == 0 {
		return nil
	}
	sorted := append([]int(nil), order...)
	sort.Ints(sorted)
	calls := make([]ToolCall, 0, len(sorted))
	for _, idx := range sorted {
		calls = append(calls, *byIndex[idx])
	}
	return calls
}

func parseRetryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			secs = 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d < 0 {
			d = 0
		}
		return d
	}
	return 0
}
