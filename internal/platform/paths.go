package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/10kkyvl/studioforge/internal/config"
)

func DataDir(override string) (string, error) {
	if override != "" {
		if err := os.MkdirAll(override, 0o700); err != nil {
			return "", fmt.Errorf("create data directory: %w", err)
		}
		return filepath.Clean(override), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	dir := filepath.Join(base, config.ProductName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create data directory: %w", err)
	}
	return dir, nil
}

func EnsurePrivateDirs(dataDir string) error {
	for _, name := range []string{"backups", "exports", "logs", "artifacts", "runtime", "mcp"} {
		if err := os.MkdirAll(filepath.Join(dataDir, name), 0o700); err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
	}
	return nil
}
