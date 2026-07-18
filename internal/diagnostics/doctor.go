package diagnostics

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/10kkyvl/studioforge/internal/config"
	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers/claudecode"
	"github.com/10kkyvl/studioforge/internal/providers/codex"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
	"github.com/10kkyvl/studioforge/internal/rojo"
	"github.com/10kkyvl/studioforge/internal/security"
)

type Doctor struct {
	DB                 *database.DB
	DataDir            string
	SafeMode, MockMode bool
	Claude             *claudecode.Provider
	Codex              *codex.Provider
	Rojo               *rojo.Manager
	MCPOverride        string
	GitOverride        string
	mu                 sync.RWMutex
}

func (d *Doctor) SetMCPOverride(path string) {
	d.mu.Lock()
	d.MCPOverride = path
	d.mu.Unlock()
}
func (d *Doctor) SetGitOverride(path string) {
	d.mu.Lock()
	d.GitOverride = path
	d.mu.Unlock()
}

func (d *Doctor) Run(ctx context.Context) models.Diagnostics {
	report := models.Diagnostics{Version: config.Version, Commit: config.Commit, BuildDate: config.BuildDate, OS: runtime.GOOS, Arch: runtime.GOARCH, DataPath: d.DataDir, SafeMode: d.SafeMode, MockMode: d.MockMode, Dependencies: map[string]models.Check{}}
	if d.DB != nil {
		if err := d.DB.Integrity(ctx); err != nil {
			report.Database = "error"
			report.Checks = append(report.Checks, models.Check{Name: "database", Status: "error", Message: err.Error()})
		} else {
			report.Database = "ok"
			report.Checks = append(report.Checks, models.Check{Name: "database", Status: "ok", Message: "Integrity and foreign keys verified"})
		}
		report.WAL = d.DB.JournalMode(ctx) == "wal"
		report.FTS5 = d.DB.FTS5
	}
	d.mu.RLock()
	gitExecutable, mcpOverride := d.GitOverride, d.MCPOverride
	d.mu.RUnlock()
	if gitExecutable == "" {
		gitExecutable = "git"
	}
	report.Dependencies["git"] = executableCheck(ctx, gitExecutable, []string{"--version"}, "Install Git or configure its executable path in Settings.")
	if d.Codex != nil {
		v := d.Codex.Diagnose(ctx)
		status := "missing"
		if v.Path != "" && !v.Available {
			status = "error"
		}
		if v.Available {
			status = "ok"
		}
		if v.Available && !v.Authenticated {
			status = "warning"
		}
		report.Dependencies["codex"] = models.Check{Name: "Codex CLI", Status: status, Version: v.Version, Path: v.Path, Message: v.Message, Help: "Run `codex login`, or configure the Codex executable path in Settings."}
	}
	if d.Claude != nil {
		v := d.Claude.Diagnose(ctx)
		status := "missing"
		if v.Path != "" && !v.Available {
			status = "error"
		}
		if v.Available {
			status = "ok"
		}
		if v.Available && !v.Authenticated {
			status = "warning"
		}
		report.Dependencies["claude"] = models.Check{Name: "Claude Code", Status: status, Version: v.Version, Path: v.Path, Message: v.Message, Help: "Run `claude auth status`, then authenticate with Claude Code if needed."}
	}
	if d.Rojo != nil {
		v := d.Rojo.Diagnose(ctx)
		status := "missing"
		if v.Available {
			status = "ok"
		}
		report.Dependencies["rojo"] = models.Check{Name: "Rojo", Status: status, Version: v.Version, Path: v.Path, Message: v.Message, Help: "Install Rojo 7 from the official Rojo documentation."}
	}
	launch, err := mcp.DetectLauncher(mcpOverride)
	if err != nil {
		report.Dependencies["studioMcp"] = models.Check{Name: "Roblox Studio MCP", Status: "missing", Message: err.Error(), Help: "Update Roblox Studio, open Assistant settings, and enable Studio as MCP server."}
	} else {
		report.Dependencies["studioMcp"] = models.Check{Name: "Roblox Studio MCP", Status: "ok", Path: launch.Command, Message: "Official Studio MCP launcher detected"}
	}
	testPath := filepath.Join(d.DataDir, "runtime", "doctor-write-test")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o700); err == nil {
		err = os.WriteFile(testPath, []byte("ok"), 0o600)
		_ = os.Remove(testPath)
	}
	if err != nil {
		report.Checks = append(report.Checks, models.Check{Name: "dataDirectory", Status: "error", Message: err.Error()})
	} else {
		report.Checks = append(report.Checks, models.Check{Name: "dataDirectory", Status: "ok", Message: "Data directory is writable"})
	}
	return report
}
func executableCheck(ctx context.Context, name string, args []string, help string) models.Check {
	path, err := exec.LookPath(name)
	if err != nil {
		return models.Check{Name: name, Status: "missing", Message: name + " was not found on PATH", Help: help}
	}
	out, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	if err != nil {
		return models.Check{Name: name, Status: "error", Path: path, Message: strings.TrimSpace(string(out)), Help: help}
	}
	return models.Check{Name: name, Status: "ok", Path: path, Version: strings.TrimSpace(string(out))}
}

func (d *Doctor) ExportBundle(ctx context.Context, target string) error {
	report := d.Run(ctx)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	file, err := os.Create(target)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(file)
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(target)
		}
	}()
	write := func(name string, value any) error {
		entry, err := zw.Create(name)
		if err != nil {
			return err
		}
		body, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		_, err = io.WriteString(entry, security.Redact(string(body))+"\n")
		return err
	}
	if err := write("doctor.json", report); err != nil {
		return err
	}
	meta := map[string]any{"generatedAt": time.Now().UTC(), "note": "Secrets, environment variables, prompts, and project source are not included."}
	if err := write("README.json", meta); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func JSON(report models.Diagnostics) string {
	body, _ := json.MarshalIndent(report, "", "  ")
	return fmt.Sprintln(string(body))
}
