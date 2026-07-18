package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func TestStdioTransportHandshakeDiscoveryAndCall(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	transport := newStdioTransport(clientWriter, clientReader)
	t.Cleanup(func() {
		_ = transport.Close()
		_ = serverReader.Close()
		_ = serverWriter.Close()
	})

	go serveMCPFixture(t, serverReader, serverWriter)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := transport.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	tools, err := transport.ListTools(ctx)
	if err != nil || len(tools) != 1 || tools[0].Name != "list_roblox_studios" {
		t.Fatalf("tools=%+v err=%v", tools, err)
	}
	raw, err := transport.Call(ctx, "list_roblox_studios", nil)
	var callResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	decodeErr := json.Unmarshal(raw, &callResult)
	if err != nil || decodeErr != nil || len(callResult.Content) != 1 || callResult.Content[0].Type != "text" || callResult.Content[0].Text != "[]" {
		t.Fatalf("result=%s err=%v", raw, err)
	}
}

func serveMCPFixture(t *testing.T, reader io.Reader, writer io.Writer) {
	t.Helper()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if len(request.ID) == 0 { // initialized notification
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{}, "serverInfo": map[string]string{"name": "fixture", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []Tool{{Name: "list_roblox_studios"}}}
		case "tools/call":
			result = map[string]any{"content": []map[string]string{{"type": "text", "text": "[]"}}}
		default:
			t.Errorf("unexpected method %s", request.Method)
			return
		}
		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		if _, err := writer.Write(append(response, '\n')); err != nil {
			return
		}
	}
}
