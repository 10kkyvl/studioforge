package diagnostics

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/processes"
)

func TestExportBundleWritesAValidZipAndClosesTheFile(t *testing.T) {
	d := &Doctor{}
	target := filepath.Join(t.TempDir(), "bundle.zip")
	if err := d.ExportBundle(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(target)
	if err != nil {
		t.Fatalf("zip.OpenReader: %v", err)
	}
	names := map[string]bool{}
	for _, f := range r.File {
		names[f.Name] = true
	}
	r.Close()
	if !names["doctor.json"] || !names["README.json"] {
		t.Fatalf("bundle entries = %v, want doctor.json and README.json", names)
	}
	if err := os.Remove(target); err != nil {
		t.Fatalf("target file left open after export: %v", err)
	}
}

func TestConfinementCheckReportsPlatformScopeHonestly(t *testing.T) {
	check := confinementCheck()

	wantStatus := "ok"
	if err := processes.ProbeConfinement(); err != nil {
		wantStatus = "error"
	}
	if check.Status != wantStatus {
		t.Errorf("status = %q, want %q (message=%q)", check.Status, wantStatus, check.Message)
	}

	if runtime.GOOS == "windows" {
		if !strings.Contains(check.Message, "filesystem is NOT confined") {
			t.Errorf("message = %q, want it to plainly say the filesystem is NOT confined on windows", check.Message)
		}
	}
}

func TestNetworkPolicyCheckReportsWhatThePlatformEnforces(t *testing.T) {
	check := networkPolicyCheck()

	wantStatus := "ok"
	if runtime.GOOS == "darwin" {
		if err := processes.EnforcesNetworkPolicy(processes.NetworkNone); err != nil {
			wantStatus = "error"
		}
	}
	if check.Status != wantStatus {
		t.Errorf("status = %q, want %q (message=%q)", check.Status, wantStatus, check.Message)
	}

	if runtime.GOOS == "windows" {
		if !strings.Contains(check.Message, "refuse to start the agent's command") {
			t.Errorf("message = %q, want it to plainly say a stricter policy makes StudioForge refuse to start the agent's command", check.Message)
		}
	}
}

func TestDoctorNeverEmbedsSecretsInCheckMessages(t *testing.T) {
	const openrouterSecret = "sk-or-v1-OPENROUTERSECRETVALUE1234567890ABCDEF"
	const nvidiaSecret = "nvapi-NVIDIASECRETVALUE1234567890ABCDEFGHIJ"

	d := &Doctor{
		DataDir:            t.TempDir(),
		OpenRouterKeyState: func(context.Context) string { return openrouterSecret },
		NVIDIAKeyState:     func(context.Context) string { return nvidiaSecret },
	}
	report := d.Run(context.Background())

	check := func(name, message string) {
		t.Helper()
		if strings.Contains(message, openrouterSecret) {
			t.Errorf("check %q leaks the OpenRouter secret in its message: %q", name, message)
		}
		if strings.Contains(message, nvidiaSecret) {
			t.Errorf("check %q leaks the NVIDIA secret in its message: %q", name, message)
		}
	}
	for _, c := range report.Checks {
		check(c.Name, c.Message)
	}
	for name, c := range report.Dependencies {
		check(name, c.Message)
	}
}
