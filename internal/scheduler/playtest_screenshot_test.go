package scheduler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/attachments"
)

// storedScreenshot writes an image the way the validator now does and returns
// the reference a ValidationResult would carry.
func storedScreenshot(t *testing.T) (root, ref string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(attachments.Dir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "2026-07-26-abc123def456.png"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, attachments.Dir + "/" + name
}

// The correction run used to be handed a path in prose and no image, because
// attachments were per-user-turn and a correction is system-initiated — which
// excluded exactly the turn that needs the picture most.
func TestCorrectionCarriesTheScreenshotAsAnAttachment(t *testing.T) {
	root, ref := storedScreenshot(t)
	parent := &Job{RunID: "run-1", ProjectID: "p1", WorkingDirectory: root}
	correction := buildCorrectionJob(parent, "session-1", ValidationResult{
		Outcome: ValidationFailed, Errors: []string{"attempt to index nil"}, Screenshot: ref,
	})

	if len(correction.Attachments) != 1 || correction.Attachments[0] != ref {
		t.Fatalf("Attachments=%v, want the stored screenshot", correction.Attachments)
	}
	if !strings.Contains(correction.Prompt, attachments.PromptHeader) {
		t.Error("the prompt must carry the attachments block so the image renders and Claude can read it")
	}
	if !strings.Contains(correction.Prompt, ref) {
		t.Error("the prompt must name the screenshot path")
	}
}

// A Studio that returned a path rather than an image leaves nothing to attach,
// and the path stays in the prose exactly where it always was.
func TestCorrectionAttachesNothingForABarePath(t *testing.T) {
	parent := &Job{RunID: "run-1", ProjectID: "p1", WorkingDirectory: t.TempDir()}
	correction := buildCorrectionJob(parent, "session-1", ValidationResult{
		Outcome: ValidationFailed, Errors: []string{"boom"}, Screenshot: `C:\shots\1.png`,
	})
	if len(correction.Attachments) != 0 {
		t.Fatalf("Attachments=%v, want none for a path that is not a stored image", correction.Attachments)
	}
	if strings.Contains(correction.Prompt, attachments.PromptHeader) {
		t.Error("no attachments block should appear when nothing was attached")
	}
	if !strings.Contains(correction.Prompt, `C:\shots\1.png`) {
		t.Error("the bare path must still be mentioned in the prose")
	}
}

// A reference pointing outside the project is refused rather than attached,
// which is the same containment rule that guards a pasted image.
func TestCorrectionRefusesAScreenshotOutsideTheProject(t *testing.T) {
	parent := &Job{RunID: "run-1", ProjectID: "p1", WorkingDirectory: t.TempDir()}
	for _, ref := range []string{
		attachments.Dir + "/../../secret.png",
		attachments.Dir + "/",
		"/etc/passwd",
	} {
		correction := buildCorrectionJob(parent, "s", ValidationResult{Outcome: ValidationFailed, Screenshot: ref})
		if len(correction.Attachments) != 0 {
			t.Errorf("Attachments=%v for %q, want none", correction.Attachments, ref)
		}
	}
}

func TestCorrectionWithNoScreenshotIsUnchanged(t *testing.T) {
	parent := &Job{RunID: "run-1", ProjectID: "p1", WorkingDirectory: t.TempDir()}
	correction := buildCorrectionJob(parent, "session-1", ValidationResult{Outcome: ValidationFailed, Errors: []string{"boom"}})
	if len(correction.Attachments) != 0 {
		t.Fatalf("Attachments=%v, want none", correction.Attachments)
	}
	if correction.Prompt == "" {
		t.Fatal("the correction prompt must still describe the failure")
	}
	if strings.Contains(correction.Prompt, attachments.PromptHeader) {
		t.Error("no attachments block should appear when there was no screenshot")
	}
}
