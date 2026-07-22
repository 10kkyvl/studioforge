package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const DefaultBaseURL = "https://openrouter.ai/api/v1"

const maxResponseBytes = 32 * 1024 * 1024

func Fetch(ctx context.Context, httpClient *http.Client, baseURL string) ([]Model, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	url := strings.TrimRight(baseURL, "/") + "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build models request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read models response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch models: unexpected status %d", resp.StatusCode)
	}

	var raw struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]Model, 0, len(raw.Data))
	for _, item := range raw.Data {
		var m Model
		if err := json.Unmarshal(item, &m); err != nil {
			continue
		}
		models = append(models, m)
	}
	return models, nil
}
