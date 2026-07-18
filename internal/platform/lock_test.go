package platform

import (
	"errors"
	"testing"
)

func TestSingleInstanceLock(t *testing.T) {
	dir := t.TempDir()
	first, err := AcquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(dir); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second lock error=%v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = second.Release()
}
