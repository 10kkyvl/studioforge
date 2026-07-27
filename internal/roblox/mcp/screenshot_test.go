package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/attachments"
)

var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

// imagePlaytestTransport answers screen_capture with a real image block, the
// way the launcher does, instead of the plain text the older fake returned.
type imagePlaytestTransport struct {
	playtestTransport
	imageData    string
	captureOrder int // how many calls had already happened when the capture ran
}

func (p *imagePlaytestTransport) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	if name == "screen_capture" {
		p.totalCalls++
		p.screenshotCalls++
		p.captureOrder = p.consoleCalls
		if p.imageData == "" {
			return json.RawMessage(`{"content":[{"type":"text","text":"C:\\shots\\1.png"}]}`), nil
		}
		return json.RawMessage(`{"content":[{"type":"image","data":"` + p.imageData + `","mimeType":"image/png"}]}`), nil
	}
	return p.playtestTransport.Call(ctx, name, args)
}

func TestImageResult(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(tinyPNG)
	raw := json.RawMessage(`{"content":[{"type":"text","text":"captured"},{"type":"image","data":"` + encoded + `","mimeType":"image/png"}]}`)
	got, err := ImageResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(tinyPNG) {
		t.Fatal("the decoded image is not the one sent")
	}
	for name, bad := range map[string]json.RawMessage{
		"text only":  json.RawMessage(`{"content":[{"type":"text","text":"hi"}]}`),
		"tool error": json.RawMessage(`{"content":[{"type":"image","data":"` + encoded + `"}],"isError":true}`),
		"bad base64": json.RawMessage(`{"content":[{"type":"image","data":"%%%%"}]}`),
		"not json":   json.RawMessage(`nope`),
	} {
		if _, err := ImageResult(bad); err == nil {
			t.Errorf("ImageResult accepted %s", name)
		}
	}
}

// The capture used to run the instant Play mode was entered, before the place
// had loaded, so the one visual signal the loop collects was of a loading
// screen. It now runs after the console window has elapsed.
func TestValidateCapturesAfterTheConsoleWindow(t *testing.T) {
	transport := &imagePlaytestTransport{
		playtestTransport: playtestTransport{
			studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
			consoleResponses: []string{"[Output] Server started", "[Output] Player joined"},
		},
		imageData: base64.StdEncoding.EncodeToString(tinyPNG),
	}
	p := newProvisioner(t, transport)
	result := p.Validate(context.Background(), fastValidateRequest())
	if result.Outcome != ValidationNoErrors {
		t.Fatalf("outcome=%q notice=%q", result.Outcome, result.Notice)
	}
	if transport.screenshotCalls != 1 {
		t.Fatalf("screenshotCalls=%d, want exactly 1", transport.screenshotCalls)
	}
	if transport.captureOrder == 0 {
		t.Fatal("the screenshot was taken before any console poll — that is the old, too-early capture")
	}
	if transport.captureOrder < transport.consoleCalls {
		t.Errorf("the screenshot ran after %d of %d console polls, want it last", transport.captureOrder, transport.consoleCalls)
	}
}

func TestValidateStoresTheScreenshotAsAnAttachment(t *testing.T) {
	root := t.TempDir()
	transport := &imagePlaytestTransport{
		playtestTransport: playtestTransport{
			studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
			consoleResponses: []string{"Server started"},
		},
		imageData: base64.StdEncoding.EncodeToString(tinyPNG),
	}
	p := newProvisioner(t, transport)
	req := fastValidateRequest()
	req.ProjectPath = root
	result := p.Validate(context.Background(), req)

	if !strings.HasPrefix(result.Screenshot, attachments.Dir+"/") {
		t.Fatalf("screenshot=%q, want a path under %q", result.Screenshot, attachments.Dir)
	}
	if !attachments.ValidRef(root, result.Screenshot) {
		t.Fatalf("screenshot=%q does not resolve inside the project", result.Screenshot)
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(result.Screenshot)))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(tinyPNG) {
		t.Fatal("the stored image is not the captured one")
	}
}

// Without a project path there is nowhere to put an image, and a Studio that
// answers with text has no image to put anywhere. Both keep the old behaviour
// rather than losing the reference entirely.
func TestValidateFallsBackToTheTextReference(t *testing.T) {
	for name, req := range map[string]ValidateRequest{
		"no project path": fastValidateRequest(),
	} {
		t.Run(name, func(t *testing.T) {
			transport := &imagePlaytestTransport{
				playtestTransport: playtestTransport{
					studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
					consoleResponses: []string{"Server started"},
				},
				imageData: base64.StdEncoding.EncodeToString(tinyPNG),
			}
			p := newProvisioner(t, transport)
			if got := p.Validate(context.Background(), req).Screenshot; got != "" && strings.HasPrefix(got, attachments.Dir) {
				t.Fatalf("screenshot=%q, want no attachment without a project path", got)
			}
		})
	}

	transport := &imagePlaytestTransport{
		playtestTransport: playtestTransport{
			studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
			consoleResponses: []string{"Server started"},
		},
		// A Studio that returns a path instead of an image.
		imageData: "",
	}
	p := newProvisioner(t, transport)
	req := fastValidateRequest()
	req.ProjectPath = t.TempDir()
	if got := p.Validate(context.Background(), req).Screenshot; got != `C:\shots\1.png` {
		t.Fatalf("screenshot=%q, want the text reference to survive", got)
	}
}

// A screenshot is never worth failing a run over: the outcome is decided by the
// console, and a capture that cannot happen just costs the visual signal.
func TestValidateStaysFailOpenWhenTheCaptureIsUnusable(t *testing.T) {
	transport := &imagePlaytestTransport{
		playtestTransport: playtestTransport{
			studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
			consoleResponses: []string{"[Output] Server started"},
		},
		imageData: "%%%not-base64%%%",
	}
	p := newProvisioner(t, transport)
	req := fastValidateRequest()
	req.ProjectPath = t.TempDir()
	result := p.Validate(context.Background(), req)
	if result.Outcome != ValidationNoErrors {
		t.Fatalf("outcome=%q, want the console to decide the outcome", result.Outcome)
	}
	if result.Screenshot != "" {
		t.Errorf("screenshot=%q, want none when the image could not be decoded", result.Screenshot)
	}
}
