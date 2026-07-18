package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrAlreadyRunning = errors.New("another StudioForge instance is using this data directory")

type Lock struct {
	path string
	file *os.File
}
type lockInfo struct {
	PID        int    `json:"pid"`
	AcquiredAt string `json:"acquiredAt"`
}

func AcquireLock(dataDir string) (*Lock, error) {
	path := filepath.Join(dataDir, "studioforge.lock")
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			info, _ := json.Marshal(lockInfo{PID: os.Getpid(), AcquiredAt: time.Now().UTC().Format(time.RFC3339Nano)})
			if _, err = file.Write(info); err != nil {
				file.Close()
				os.Remove(path)
				return nil, fmt.Errorf("write instance lock: %w", err)
			}
			if err = file.Sync(); err != nil {
				file.Close()
				os.Remove(path)
				return nil, fmt.Errorf("sync instance lock: %w", err)
			}
			return &Lock{path: path, file: file}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire instance lock: %w", err)
		}
		body, readErr := os.ReadFile(path)
		var info lockInfo
		if readErr == nil && json.Unmarshal(body, &info) == nil && info.PID > 0 && processAlive(info.PID) {
			return nil, ErrAlreadyRunning
		}
		if removeErr := os.Remove(path); removeErr != nil {
			return nil, fmt.Errorf("remove stale instance lock: %w", removeErr)
		}
	}
	return nil, ErrAlreadyRunning
}

func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	return os.Remove(l.path)
}
