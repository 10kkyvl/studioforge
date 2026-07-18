package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

const protocolVersion = "2025-06-18"

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// codeMethodNotFound is the JSON-RPC code for an unimplemented method. It is
// the only response that means "this Studio is too old to offer the tool",
// which callers must tell apart from an ordinary call failure.
const codeMethodNotFound = -32601

// Error reports a JSON-RPC error returned by the Studio MCP server. The code is
// kept, rather than flattened into the message, so callers can distinguish a
// missing method from a failed one.
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("Studio MCP error %d: %s", e.Code, e.Message) }

// IsMethodNotFound reports whether the server does not implement the method at
// all, as opposed to implementing it and failing.
func IsMethodNotFound(err error) bool {
	var rpc *Error
	return errors.As(err, &rpc) && rpc.Code == codeMethodNotFound
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcReply struct {
	result json.RawMessage
	err    error
}

// StdioTransport implements the official MCP JSON-RPC stdio protocol. One
// transport owns one StudioMCP child process and is safe for concurrent calls.
type StdioTransport struct {
	stdin  io.WriteCloser
	cmd    *exec.Cmd
	stderr *lockedBuffer

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[string]chan rpcReply
	done    chan struct{}
	closed  sync.Once
	err     error
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// NewStdioTransport launches the detected Studio MCP command and completes the
// MCP initialize handshake before returning.
func NewStdioTransport(ctx context.Context, launch LaunchConfig) (*StdioTransport, error) {
	if launch.Command == "" {
		return nil, errors.New("Studio MCP command is required")
	}
	cmd := exec.CommandContext(ctx, launch.Command, launch.Args...)
	// On Windows the launcher is `cmd.exe /c mcp.bat`, which runs StudioMCP.exe
	// as a child. Killing the launcher leaves that grandchild holding the stderr
	// pipe, and because Stderr is a buffer rather than a file, Wait blocks on the
	// copy goroutine until every write handle closes. Without a delay that wait
	// is unbounded, and it runs while a scheduler slot and a project write lease
	// are held.
	cmd.WaitDelay = 5 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("Studio MCP stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("Studio MCP stdout: %w", err)
	}
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Studio MCP: %w", err)
	}
	t := &StdioTransport{
		stdin:   stdin,
		cmd:     cmd,
		stderr:  stderr,
		pending: map[string]chan rpcReply{},
		done:    make(chan struct{}),
	}
	go t.readLoop(stdout)
	if err := t.initialize(ctx); err != nil {
		_ = t.Close()
		return nil, err
	}
	return t, nil
}

func newStdioTransport(stdin io.WriteCloser, stdout io.Reader) *StdioTransport {
	t := &StdioTransport{stdin: stdin, pending: map[string]chan rpcReply{}, done: make(chan struct{})}
	go t.readLoop(stdout)
	return t
}

func (t *StdioTransport) initialize(ctx context.Context) error {
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	raw, err := t.request(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "StudioForge",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("initialize Studio MCP: %w", err)
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode Studio MCP initialize response: %w", err)
	}
	if result.ProtocolVersion == "" {
		return errors.New("Studio MCP initialize response omitted protocolVersion")
	}
	if err := t.notify("notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("finish Studio MCP initialization: %w", err)
	}
	return nil
}

func (t *StdioTransport) ListTools(ctx context.Context) ([]Tool, error) {
	raw, err := t.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode Studio MCP tools: %w", err)
	}
	if result.Tools == nil {
		result.Tools = make([]Tool, 0)
	}
	return result.Tools, nil
}

func (t *StdioTransport) Call(ctx context.Context, name string, arguments map[string]any) (json.RawMessage, error) {
	if arguments == nil {
		arguments = map[string]any{}
	}
	return t.request(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments})
}

func (t *StdioTransport) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	t.nextID++
	id := t.nextID
	key := strconv.FormatInt(id, 10)
	reply := make(chan rpcReply, 1)
	t.pending[key] = reply
	t.mu.Unlock()

	message := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	if err := t.write(message); err != nil {
		t.removePending(key)
		return nil, err
	}
	select {
	case response := <-reply:
		return response.result, response.err
	case <-ctx.Done():
		t.removePending(key)
		return nil, ctx.Err()
	case <-t.done:
		return nil, t.transportError()
	}
}

func (t *StdioTransport) notify(method string, params any) error {
	return t.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (t *StdioTransport) write(message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	select {
	case <-t.done:
		return t.transportError()
	default:
	}
	if _, err := t.stdin.Write(body); err != nil {
		return fmt.Errorf("write Studio MCP request: %w", err)
	}
	return nil
}

func (t *StdioTransport) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var response rpcResponse
		if err := json.Unmarshal(line, &response); err != nil || len(response.ID) == 0 {
			continue // Server notifications and non-protocol log lines are not replies.
		}
		key := string(response.ID)
		t.mu.Lock()
		waiter := t.pending[key]
		delete(t.pending, key)
		t.mu.Unlock()
		if waiter == nil {
			continue
		}
		if response.Error != nil {
			waiter <- rpcReply{err: &Error{Code: response.Error.Code, Message: response.Error.Message}}
		} else {
			waiter <- rpcReply{result: response.Result}
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	t.fail(err)
}

func (t *StdioTransport) fail(err error) {
	t.closed.Do(func() {
		t.mu.Lock()
		t.err = err
		pending := t.pending
		t.pending = map[string]chan rpcReply{}
		t.mu.Unlock()
		for _, waiter := range pending {
			waiter <- rpcReply{err: t.transportError()}
		}
		close(t.done)
	})
}

func (t *StdioTransport) removePending(key string) {
	t.mu.Lock()
	delete(t.pending, key)
	t.mu.Unlock()
}

func (t *StdioTransport) transportError() error {
	t.mu.Lock()
	err := t.err
	t.mu.Unlock()
	message := "Studio MCP transport closed"
	if err != nil && !errors.Is(err, io.EOF) {
		message += ": " + err.Error()
	}
	if t.stderr != nil && t.stderr.String() != "" {
		message += ": " + t.stderr.String()
	}
	return errors.New(message)
}

func (t *StdioTransport) Close() error {
	_ = t.stdin.Close()
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}
	select {
	case <-t.done:
	default:
		t.fail(io.EOF)
	}
	return nil
}
