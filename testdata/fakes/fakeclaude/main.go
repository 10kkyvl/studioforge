package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "--version"):
		fmt.Println("2.1.999 (Fake Claude Code)")
		return
	case strings.Contains(joined, "--help"):
		fmt.Println("--output-format stream-json --include-partial-messages --session-id --resume --model --effort --max-turns --max-budget-usd --mcp-config --strict-mcp-config --permission-mode --allowedTools --disallowedTools --json-schema --name")
		return
	case strings.Contains(joined, "auth status"):
		fmt.Println("authenticated as fake@example.invalid")
		return
	}
	scenario := os.Getenv("FAKE_CLAUDE_SCENARIO")
	if scenario == "hang" {
		for {
			time.Sleep(time.Second)
		}
	}
	events := []map[string]any{{"type": "system", "subtype": "init", "session_id": "fake-session"}, {"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "partial"}}}}, {"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "content": "ok"}}}}, {"type": "result", "session_id": "fake-session", "total_cost_usd": 0.12, "result": "done"}}
	for i, event := range events {
		if scenario == "malformed" && i == 2 {
			fmt.Println("{not-json")
			continue
		}
		body, _ := json.Marshal(event)
		fmt.Println(string(body))
		time.Sleep(10 * time.Millisecond)
	}
	if scenario == "auth_error" {
		fmt.Fprintln(os.Stderr, "authentication required")
		os.Exit(1)
	}
	if scenario == "rate_limit" {
		fmt.Fprintln(os.Stderr, "rate limit exceeded")
		os.Exit(1)
	}
	if scenario == "budget" {
		fmt.Fprintln(os.Stderr, "budget exceeded")
		os.Exit(1)
	}
	if scenario == "crash" {
		os.Exit(17)
	}
}
