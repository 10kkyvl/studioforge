package diagnostics

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
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
