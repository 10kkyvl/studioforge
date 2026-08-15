package projects

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrepareRootCreatesOneLevelUnderAnExistingParent(t *testing.T) {
	parent := t.TempDir()
	data := t.TempDir()
	target := filepath.Join(parent, "newproj")
	root, err := PrepareRoot(target, data, true)
	if err != nil {
		t.Fatalf("PrepareRoot returned an unexpected error: %v", err)
	}
	info, statErr := os.Stat(root)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("expected %s to exist as a directory, stat error: %v", root, statErr)
	}
}

func TestPrepareRootRefusesWhenParentDoesNotExist(t *testing.T) {
	base := t.TempDir()
	data := t.TempDir()
	target := filepath.Join(base, "missing-parent", "project")
	_, err := PrepareRoot(target, data, true)
	if !errors.Is(err, ErrProjectParentMissing) {
		t.Fatalf("expected ErrProjectParentMissing, got %v", err)
	}
}

func TestPrepareRootRefusesAVolumeRoot(t *testing.T) {
	data := t.TempDir()
	var volumeRoot string
	if runtime.GOOS == "windows" {
		volumeRoot = filepath.VolumeName(data) + `\`
	} else {
		volumeRoot = "/"
	}
	_, err := PrepareRoot(volumeRoot, data, false)
	if !errors.Is(err, ErrUnsafeProjectRoot) {
		t.Fatalf("expected ErrUnsafeProjectRoot, got %v", err)
	}
}

func TestPrepareRootRefusesSystemDirectories(t *testing.T) {
	data := t.TempDir()
	var cases []string
	if runtime.GOOS == "windows" {
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			systemRoot = `C:\Windows`
		}
		cases = []string{systemRoot, filepath.Join(systemRoot, "System32")}
	} else {
		cases = []string{"/etc", "/usr"}
	}
	for _, path := range cases {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Skipf("system path %s not present on this machine: %v", path, statErr)
		}
		t.Run(path, func(t *testing.T) {
			_, err := PrepareRoot(path, data, false)
			if !errors.Is(err, ErrUnsafeProjectRoot) {
				t.Fatalf("expected ErrUnsafeProjectRoot for %s, got %v", path, err)
			}
		})
	}
}

func TestPrepareRootRefusesInsideTheDataDirectory(t *testing.T) {
	data := t.TempDir()
	target := filepath.Join(data, "myproject")
	_, err := PrepareRoot(target, data, true)
	if !errors.Is(err, ErrUnsafeProjectRoot) {
		t.Fatalf("expected ErrUnsafeProjectRoot, got %v", err)
	}
}

func TestPrepareRootAcceptsAnExistingDirectoryWithoutCreate(t *testing.T) {
	existing := t.TempDir()
	data := t.TempDir()
	root, err := PrepareRoot(existing, data, false)
	if err != nil {
		t.Fatalf("PrepareRoot returned an unexpected error: %v", err)
	}
	if _, statErr := os.Stat(root); statErr != nil {
		t.Fatalf("expected %s to exist, stat error: %v", root, statErr)
	}
}

func TestPrepareRootCanonicalisesTheReturnedPath(t *testing.T) {
	parent := t.TempDir()
	data := t.TempDir()
	messy := filepath.Join(parent, "sub", "..", "newproj")
	root, err := PrepareRoot(messy, data, true)
	if err != nil {
		t.Fatalf("PrepareRoot returned an unexpected error: %v", err)
	}
	want, err := Canonical(filepath.Join(parent, "newproj"))
	if err != nil {
		t.Fatalf("Canonical returned an unexpected error: %v", err)
	}
	if root != want {
		t.Fatalf("root=%s want=%s", root, want)
	}
}
