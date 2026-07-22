package openrouter

import (
	"encoding/base64"
	"net/http"
	"os"

	"github.com/10kkyvl/studioforge/internal/providers/openrouter/agenttools"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/orclient"
)

const maxImageAttachmentBytes = 10 << 20

var allowedImageMIMETypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

func buildUserMessage(ws *agenttools.Workspace, prompt string, attachments []string, vision bool) (orclient.Message, error) {
	if len(attachments) == 0 || !vision {
		return orclient.Message{Role: "user", Content: prompt}, nil
	}
	parts := []orclient.ContentPart{{Type: "text", Text: prompt}}
	for _, attachment := range attachments {
		part, ok := loadImagePart(ws, attachment)
		if !ok {
			continue
		}
		parts = append(parts, part)
	}
	return orclient.Message{Role: "user", Content: parts}, nil
}

func loadImagePart(ws *agenttools.Workspace, attachment string) (orclient.ContentPart, bool) {
	abs, err := ws.Resolve(attachment)
	if err != nil {
		return orclient.ContentPart{}, false
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() || info.Size() > maxImageAttachmentBytes {
		return orclient.ContentPart{}, false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return orclient.ContentPart{}, false
	}
	mimeType := http.DetectContentType(data)
	if !allowedImageMIMETypes[mimeType] {
		return orclient.ContentPart{}, false
	}
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
	return orclient.ContentPart{Type: "image_url", ImageURL: &orclient.ImageURL{URL: dataURL}}, true
}
