package processes

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Spec struct {
	ID, Kind, ProjectID, RunID, Executable, WorkingDirectory string
	Args, Environment                                        []string
	MaxRuntime                                               time.Duration
}
type Line struct {
	Stream string
	Text   string
	At     time.Time
}
type Result struct {
	ExitCode            int
	StartedAt, ExitedAt time.Time
	Err                 error
}
type Process struct {
	spec         Spec
	cmd          *exec.Cmd
	lines        chan Line
	done         chan struct{}
	cancel       context.CancelFunc
	mu           sync.RWMutex
	result       Result
	once         sync.Once
	droppedLines atomic.Int64
	collectors   sync.WaitGroup
}
type Supervisor struct {
	mu        sync.Mutex
	processes map[string]*Process
	reserving map[string]struct{}
	closing   bool
}

func NewSupervisor() *Supervisor {
	return &Supervisor{processes: map[string]*Process{}, reserving: map[string]struct{}{}}
}

func (s *Supervisor) unreserve(id string) {
	s.mu.Lock()
	delete(s.reserving, id)
	s.mu.Unlock()
}

func (s *Supervisor) Start(parent context.Context, spec Spec) (*Process, error) {
	if spec.ID == "" || spec.Executable == "" {
		return nil, errors.New("process ID and executable are required")
	}
	ctx := parent
	cancel := func() {}
	if spec.MaxRuntime > 0 {
		ctx, cancel = context.WithTimeout(parent, spec.MaxRuntime)
	} else {
		ctx, cancel = context.WithCancel(parent)
	}
	cmd := exec.CommandContext(ctx, spec.Executable, spec.Args...)
	cmd.Dir = spec.WorkingDirectory
	if len(spec.Environment) > 0 {
		cmd.Env = append([]string(nil), spec.Environment...)
	}
	configureProcessTree(cmd)
	if spec.MaxRuntime > 0 {
		cmd.Cancel = func() error { return forceKillTree(cmd) }
		cmd.WaitDelay = 5 * time.Second
	}
	p := &Process{spec: spec, cmd: cmd, lines: make(chan Line, 256), done: make(chan struct{}), cancel: cancel, result: Result{ExitCode: -1}}

	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		cancel()
		return nil, errors.New("process supervisor is shutting down")
	}
	_, hasProcess := s.processes[spec.ID]
	_, hasReservation := s.reserving[spec.ID]
	if hasProcess || hasReservation {
		s.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("process %s already exists", spec.ID)
	}
	s.reserving[spec.ID] = struct{}{}
	s.mu.Unlock()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.unreserve(spec.ID)
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.unreserve(spec.ID)
		cancel()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		s.unreserve(spec.ID)
		cancel()
		return nil, fmt.Errorf("start %s: %w", spec.Kind, err)
	}
	p.result.StartedAt = time.Now().UTC()

	s.mu.Lock()
	delete(s.reserving, spec.ID)
	closing := s.closing
	if !closing {
		s.processes[spec.ID] = p
	}
	s.mu.Unlock()

	p.collectors.Add(2)
	go p.collect(stdout, "stdout")
	go p.collect(stderr, "stderr")
	go func() {
		err := cmd.Wait()
		p.collectors.Wait()
		p.mu.Lock()
		p.result.Err = err
		p.result.ExitedAt = time.Now().UTC()
		if cmd.ProcessState != nil {
			p.result.ExitCode = cmd.ProcessState.ExitCode()
		}
		p.mu.Unlock()
		close(p.done)
		close(p.lines)
		cancel()
		s.mu.Lock()
		if s.processes[spec.ID] == p {
			delete(s.processes, spec.ID)
		}
		s.mu.Unlock()
	}()

	if closing {
		go func() { _ = p.Terminate(2 * time.Second) }()
		return nil, errors.New("process supervisor is shutting down")
	}
	return p, nil
}
func (p *Process) collect(reader io.Reader, stream string) {
	defer p.collectors.Done()
	r := bufio.NewReader(reader)
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			select {
			case p.lines <- Line{Stream: stream, Text: line, At: time.Now().UTC()}:
			default:
				// Nobody is draining Lines() fast enough (or at all). Drop
				// the line rather than blocking, which would backpressure
				// the bufio.Reader and, transitively, the child process's
				// stdout/stderr pipe. Log on the first drop and then only
				// every 500th, so a sustained drop doesn't flood the log but
				// also doesn't go silent after the first occurrence.
				if n := p.droppedLines.Add(1); n == 1 || n%500 == 0 {
					slog.Warn("process output buffer full, dropping lines", "process_id", p.spec.ID, "kind", p.spec.Kind, "dropped_total", n)
				}
			}
		}
		if err != nil {
			return
		}
	}
}
func (p *Process) Lines() <-chan Line  { return p.lines }
func (p *Process) DroppedLines() int64 { return p.droppedLines.Load() }
func (p *Process) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
func (p *Process) Wait() Result { <-p.done; p.mu.RLock(); defer p.mu.RUnlock(); return p.result }
func (p *Process) Terminate(grace time.Duration) error {
	var result error
	p.once.Do(func() {
		if p.cmd.Process == nil {
			return
		}
		if err := gracefulTerminate(p.cmd); err != nil {
			result = err
		}
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-p.done:
			return
		case <-timer.C:
			_ = forceKillTree(p.cmd)
			p.cancel()
			// Wait for the reaper goroutine to finish so a nil return from
			// Terminate reliably means the process has actually been
			// reaped, not just asked to die. Bound the wait so Terminate
			// itself cannot hang forever if something is truly stuck.
			safety := time.NewTimer(10 * time.Second)
			defer safety.Stop()
			select {
			case <-p.done:
			case <-safety.C:
			}
		}
	})
	return result
}

// ConfigureTree prepares a command the supervisor does not own so it can later
// be killed together with everything it spawns. Providers that launch a CLI
// which itself starts subprocesses (the MCP shim, tool processes) must call
// this before Start; on POSIX there is otherwise no process group to signal.
func ConfigureTree(cmd *exec.Cmd) { configureProcessTree(cmd) }

// TerminateTree stops cmd and every process below it, mirroring what
// (*Process).Terminate does for supervised processes: ask politely first so the
// CLI can flush its output and exit on its own, then force-kill the whole tree
// once grace has elapsed. exited must be closed once the command has been
// reaped; closing it cancels the escalation, which also keeps us from
// force-killing a PID the OS has since handed to somebody else.
//
// Only the graceful signal is sent synchronously, so callers learn immediately
// whether it failed. The escalation waits in the background because cancel
// paths run under locks (the scheduler holds its mutex) and must not block for
// the grace period.
func TerminateTree(cmd *exec.Cmd, exited <-chan struct{}, grace time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	go func() {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-exited:
		case <-timer.C:
			_ = forceKillTree(cmd)
		}
	}()
	return gracefulTerminate(cmd)
}

func (s *Supervisor) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	items := make([]*Process, 0, len(s.processes))
	for _, p := range s.processes {
		items = append(items, p)
	}
	s.mu.Unlock()
	for _, p := range items {
		_ = p.Terminate(2 * time.Second)
	}
	for _, p := range items {
		select {
		case <-p.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func MinimalEnvironment(extra []string) []string {
	allow := map[string]bool{
		"PATH": true, "PATHEXT": true, "HOME": true, "USERPROFILE": true,
		"LOCALAPPDATA": true, "APPDATA": true, "TMPDIR": true, "TMP": true,
		"TEMP": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true,
		"HTTP_PROXY": true, "HTTPS_PROXY": true,
		"NO_PROXY": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
	}
	out := []string{}
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && allow[strings.ToUpper(key)] {
			out = append(out, entry)
		}
	}
	return append(out, extra...)
}
