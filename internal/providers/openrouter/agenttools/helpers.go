package agenttools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var errStopWalk = errors.New("agenttools: stop walk")

func errResult(format string, args ...any) Result {
	return Result{IsError: true, Content: fmt.Sprintf(format, args...)}
}

func parseArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func looksBinary(data []byte) bool {
	probe := data
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if bytes.IndexByte(probe, 0) >= 0 {
		return true
	}
	if len(probe) == 0 {
		return false
	}
	if utf8.Valid(probe) {
		return false
	}
	invalid := 0
	for i := 0; i < len(probe); {
		r, size := utf8.DecodeRune(probe[i:])
		if r == utf8.RuneError && size == 1 {
			invalid++
		}
		i += size
	}
	return float64(invalid)/float64(len(probe)) > 0.3
}

func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".agenttools-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil {
		os.Remove(tmpPath)
		return writeErr
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return closeErr
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func replaceUnique(content, oldText, newText string) (string, error) {
	count := strings.Count(content, oldText)
	if count == 0 {
		return "", errors.New("text not found")
	}
	if count > 1 {
		return "", errors.New("text is not unique, add more context")
	}
	return strings.Replace(content, oldText, newText, 1), nil
}
