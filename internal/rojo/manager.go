package rojo

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/10kkyvl/studioforge/internal/processes"
)

type Diagnostics struct {
	Available bool   `json:"available"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	Message   string `json:"message"`
}
type Session struct {
	ProjectID, ProjectFile string
	Port                   int
	PID                    int
	StartedAt              time.Time
	Lines                  <-chan processes.Line
	process                *processes.Process
}
type Manager struct {
	supervisor *processes.Supervisor
	executable string
	mu         sync.Mutex
	sessions   map[string]*Session
}

func New(supervisor *processes.Supervisor, executable string) *Manager {
	if executable == "" {
		executable = "rojo"
	}
	return &Manager{supervisor: supervisor, executable: executable, sessions: map[string]*Session{}}
}
func (m *Manager) SetExecutable(executable string) {
	if strings.TrimSpace(executable) == "" {
		executable = "rojo"
	}
	m.mu.Lock()
	m.executable = executable
	m.mu.Unlock()
}
func (m *Manager) executablePath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.executable
}
func (m *Manager) Diagnose(ctx context.Context) Diagnostics {
	path, err := exec.LookPath(m.executablePath())
	if err != nil {
		return Diagnostics{Message: "Rojo CLI not found; install Rojo 7 and ensure rojo is on PATH"}
	}
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return Diagnostics{Path: path, Message: "Rojo version check failed: " + string(out)}
	}
	return Diagnostics{Available: true, Path: path, Version: strings.TrimSpace(string(out)), Message: "Rojo CLI detected"}
}

// Build compiles a Rojo project file into a binary place (.rbxl) at output. It
// runs to completion instead of starting a serve session, so a fresh project
// can be opened in Studio as a self-contained place file.
func (m *Manager) Build(ctx context.Context, projectFile, output string) error {
	if !strings.HasSuffix(projectFile, ".project.json") {
		return errors.New("Rojo project file must end with .project.json")
	}
	diag := m.Diagnose(ctx)
	if !diag.Available {
		return errors.New(diag.Message)
	}
	cmd := exec.CommandContext(ctx, diag.Path, "build", projectFile, "--output", output)
	cmd.Dir = filepath.Dir(projectFile)
	cmd.Env = processes.MinimalEnvironment(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rojo build failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// InstallPlugin installs the Rojo Studio plugin into the local Plugins folder so
// a freshly opened place can live-sync without a manual plugin setup step.
func (m *Manager) InstallPlugin(ctx context.Context) error {
	diag := m.Diagnose(ctx)
	if !diag.Available {
		return errors.New(diag.Message)
	}
	cmd := exec.CommandContext(ctx, diag.Path, "plugin", "install")
	cmd.Env = processes.MinimalEnvironment(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rojo plugin install failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func AllocatePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
func (m *Manager) Start(ctx context.Context, projectID, projectFile string) (*Session, error) {
	if filepath.Ext(projectFile) != ".json" || !strings.HasSuffix(projectFile, ".project.json") {
		return nil, errors.New("Rojo project file must end with .project.json")
	}
	m.mu.Lock()
	if _, ok := m.sessions[projectID]; ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("Rojo is already running for project %s", projectID)
	}
	m.mu.Unlock()
	diag := m.Diagnose(ctx)
	if !diag.Available {
		return nil, errors.New(diag.Message)
	}
	port, err := AllocatePort()
	if err != nil {
		return nil, err
	}
	proc, err := m.supervisor.Start(ctx, processes.Spec{ID: "rojo-" + projectID, Kind: "rojo", ProjectID: projectID, Executable: diag.Path, WorkingDirectory: filepath.Dir(projectFile), Args: []string{"serve", projectFile, "--port", fmt.Sprint(port)}, Environment: processes.MinimalEnvironment(nil)})
	if err != nil {
		return nil, err
	}
	session := &Session{ProjectID: projectID, ProjectFile: projectFile, Port: port, PID: proc.PID(), StartedAt: time.Now().UTC(), Lines: proc.Lines(), process: proc}
	m.mu.Lock()
	m.sessions[projectID] = session
	m.mu.Unlock()
	go func() { _ = proc.Wait(); m.mu.Lock(); delete(m.sessions, projectID); m.mu.Unlock() }()
	return session, nil
}
func (m *Manager) Stop(projectID string) error {
	m.mu.Lock()
	session, ok := m.sessions[projectID]
	m.mu.Unlock()
	if !ok {
		return errors.New("Rojo session is not running")
	}
	return session.process.Terminate(3 * time.Second)
}
func (m *Manager) Session(projectID string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[projectID]
	return s, ok
}
