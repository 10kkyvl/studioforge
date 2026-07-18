package main

import (
	"bufio"
	"encoding/json"
	"os"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request map[string]any
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{"tools": []map[string]any{{"name": "list_roblox_studios"}, {"name": "set_active_studio"}, {"name": "start_stop_play"}}}})
	}
}
