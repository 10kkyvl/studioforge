package claudecode

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/attachments"
	"github.com/10kkyvl/studioforge/internal/providers"
)

var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

// toolResultLine is the shape Claude Code streams a tool result in when the
// tool returned an image — a screen_capture from the Studio MCP server, in
// practice.
func toolResultLine(t *testing.T, data string) map[string]any {
	t.Helper()
	raw := `{"type":"user","message":{"role":"user","content":[
		{"type":"tool_result","tool_use_id":"toolu_1","content":[
			{"type":"text","text":"captured"},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + data + `"}}
		]}
	]}}`
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestToolResultImagesFindsAScreenshot(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(tinyPNG)
	got := toolResultImages(toolResultLine(t, encoded))
	if len(got) != 1 || got[0] != encoded {
		t.Fatalf("toolResultImages() = %v, want the one base64 payload", got)
	}
}

// The stream carries every kind of line, and this walks raw JSON, so anything
// that is not a tool result with an image block has to come back empty rather
// than panic.
func TestToolResultImagesIgnoresEverythingElse(t *testing.T) {
	for name, raw := range map[string]string{
		"assistant text":     `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
		"text-only result":   `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"ok"}]}]}}`,
		"content is string":  `{"type":"user","message":{"role":"user","content":"plain"}}`,
		"no message":         `{"type":"result","subtype":"success"}`,
		"image without data": `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"base64","data":""}}]}]}}`,
		"source not base64":  `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"url","data":"x"}}]}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var decoded map[string]any
			if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
				t.Fatal(err)
			}
			if got := toolResultImages(decoded); len(got) != 0 {
				t.Errorf("toolResultImages() = %v, want none", got)
			}
		})
	}
	if got := toolResultImages("not a map"); len(got) != 0 {
		t.Errorf("toolResultImages(string) = %v, want none", got)
	}
	if got := toolResultImages(nil); len(got) != 0 {
		t.Errorf("toolResultImages(nil) = %v, want none", got)
	}
}

func TestPublishToolImagesSavesAndAnnounces(t *testing.T) {
	root := t.TempDir()
	h := &handle{cmd: &exec.Cmd{Dir: root}, events: make(chan providers.Event, 4)}
	h.publishToolImages(providers.Event{
		Type: "tool", RawType: "user",
		Payload: toolResultLine(t, base64.StdEncoding.EncodeToString(tinyPNG)),
	})

	select {
	case event := <-h.events:
		if event.Type != "message" || event.RawType != "claude.screenshot" {
			t.Fatalf("event = %+v, want a screenshot message", event)
		}
		payload, _ := event.Payload.(map[string]any)
		text, _ := payload["text"].(string)
		if !strings.HasPrefix(text, attachments.PromptHeader) {
			t.Fatalf("text = %q, want the attachments block the chat renders", text)
		}
		name := strings.TrimPrefix(strings.TrimSpace(strings.Split(text, "\n- ")[1]), "")
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Fatalf("the announced image is not on disk: %v", err)
		}
	default:
		t.Fatal("no screenshot message was published")
	}
}

func TestPublishToolImagesStaysQuietWithoutAnImage(t *testing.T) {
	root := t.TempDir()
	h := &handle{cmd: &exec.Cmd{Dir: root}, events: make(chan providers.Event, 4)}
	h.publishToolImages(providers.Event{Type: "message", RawType: "assistant", Payload: map[string]any{"text": "no image here"}})
	h.publishToolImages(providers.Event{Type: "tool", RawType: "user", Payload: map[string]any{"message": map[string]any{"content": []any{}}}})
	select {
	case event := <-h.events:
		t.Fatalf("published %+v for an event with no image", event)
	default:
	}
}

// A run whose working directory is unknown has nowhere to put a file, and a
// screenshot is never worth failing a run over.
func TestPublishToolImagesWithoutAWorkingDirectory(t *testing.T) {
	h := &handle{cmd: &exec.Cmd{}, events: make(chan providers.Event, 2)}
	h.publishToolImages(providers.Event{
		Type: "tool", RawType: "user",
		Payload: toolResultLine(t, base64.StdEncoding.EncodeToString(tinyPNG)),
	})
	select {
	case event := <-h.events:
		t.Fatalf("published %+v with no working directory", event)
	default:
	}
}
