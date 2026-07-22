package orclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	Referer    string
	Title      string
	UserAgent  string
	MaxRetries int
}

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	referer    string
	title      string
	userAgent  string
	maxRetries int
}

func New(cfg Config) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
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
	return &Client{
		apiKey:     cfg.APIKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		referer:    referer,
		title:      title,
		userAgent:  userAgent,
		maxRetries: maxRetries,
	}
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
		return nil, &APIError{Kind: KindMalformedResponse, Message: err.Error()}
	}

	url := c.baseURL + "/chat/completions"

	var resp *http.Response
	attempt := 0
	for {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if reqErr != nil {
			return nil, classifyTransportErr(ctx, reqErr)
		}
		c.setHeaders(httpReq)

		resp, err = c.httpClient.Do(httpReq)
		if err != nil {
			apiErr := classifyTransportErr(ctx, err)
			if apiErr.Kind == KindCancelled || attempt >= c.maxRetries {
				return nil, apiErr
			}
			if !c.sleepBackoff(ctx, attempt, nil) {
				return nil, &APIError{Kind: KindCancelled, Message: ctx.Err().Error()}
			}
			attempt++
			continue
		}

		if resp.StatusCode == http.StatusOK {
			break
		}

		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		apiErr := parseErrorBody(resp.StatusCode, errBody)

		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) && attempt < c.maxRetries {
			apiErr.RetryAfter = parseRetryAfter(resp.Header)
			if !c.sleepBackoff(ctx, attempt, resp.Header) {
				return nil, &APIError{Kind: KindCancelled, Message: ctx.Err().Error()}
			}
			attempt++
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			apiErr.RetryAfter = parseRetryAfter(resp.Header)
		}
		return nil, apiErr
	}

	defer resp.Body.Close()
	return c.readStream(ctx, resp, sink)
}

func (c *Client) sleepBackoff(ctx context.Context, attempt int, header http.Header) bool {
	wait := backoffDuration(attempt)
	if header != nil && header.Get("Retry-After") != "" {
		if ra := parseRetryAfter(header); ra > wait {
			wait = ra
		}
	}
	if wait <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
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

func parseErrorBody(status int, body []byte) *APIError {
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
	return &APIError{Kind: classifyStatus(status, message), StatusCode: status, Message: message}
}

func classifyTransportErr(ctx context.Context, err error) *APIError {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &APIError{Kind: KindCancelled, Message: err.Error()}
	}
	return &APIError{Kind: KindNetworkFailure, Message: err.Error()}
}

func classifyStreamErr(ctx context.Context, err error) *APIError {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &APIError{Kind: KindCancelled, Message: err.Error()}
	}
	if errors.Is(err, bufio.ErrTooLong) {
		return &APIError{Kind: KindMalformedResponse, Message: err.Error()}
	}
	return &APIError{Kind: KindNetworkFailure, Message: err.Error()}
}

func (c *Client) readStream(ctx context.Context, resp *http.Response, sink Sink) (*Completion, error) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)

	result := &Completion{}
	toolByIndex := map[int]*ToolCall{}
	var toolOrder []int

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
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
		}

		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]
			if choice.Delta.Content != "" {
				result.Content += choice.Delta.Content
				if sink != nil {
					sink(Delta{Text: choice.Delta.Content})
				}
			}
			if choice.Delta.Reasoning != "" && sink != nil {
				sink(Delta{Reasoning: choice.Delta.Reasoning})
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

		if chunk.Error != nil || (len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason == "error") {
			result.ToolCalls = sortedToolCalls(toolByIndex, toolOrder)
			kind := KindProviderFailure
			status := 0
			msg := ""
			if chunk.Error != nil {
				status = chunk.Error.Code
				msg = chunk.Error.Message
				kind = classifyStatus(status, msg)
			}
			return result, &APIError{Kind: kind, StatusCode: status, Message: msg}
		}
	}

	if err := scanner.Err(); err != nil {
		result.ToolCalls = sortedToolCalls(toolByIndex, toolOrder)
		return result, classifyStreamErr(ctx, err)
	}

	result.ToolCalls = sortedToolCalls(toolByIndex, toolOrder)
	return result, nil
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
