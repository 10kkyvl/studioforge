package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	joined := strings.Join(os.Args[1:], " ")
	switch {
	case strings.Contains(joined, "--version"):
		fmt.Println("codex-cli 99.0.0-fake")
		return
	case strings.Contains(joined, "login status"):
		fmt.Println("Logged in using ChatGPT")
		return
	}
	if os.Getenv("FAKE_CODEX_SCENARIO") == "hang" {
		for {
			time.Sleep(time.Second)
		}
	}
	events := []map[string]any{
		{"type": "thread.started", "thread_id": "fake-thread"},
		{"type": "turn.started"},
		{"type": "item.completed", "item": map[string]any{"id": "item-1", "type": "agent_message", "text": "done"}},
		{"type": "turn.completed", "usage": map[string]any{"input_tokens": 100, "output_tokens": 20}},
	}
	for _, event := range events {
		body, _ := json.Marshal(event)
		fmt.Println(string(body))
	}
	if os.Getenv("FAKE_CODEX_SCENARIO") == "auth_error" {
		fmt.Fprintln(os.Stderr, "authentication required")
		os.Exit(1)
	}
}
