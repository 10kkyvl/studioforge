package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/10kkyvl/studioforge/internal/questions"
)

// The shim is an MCP server that StudioForge puts between an agent and the
// Roblox Studio launcher.
//
// It exists because the launcher only advertises tools to whichever process won
// the WS host port. A second launcher on the same machine is told it has zero
// tools for as long as it lives, while every call it makes still succeeds
// through the host. Claude Code builds an agent's toolset from tools/list, so an
// agent that starts while any other MCP client holds Studio would otherwise see
// no Studio tools at all and sit blind in front of a working Studio.
//
// The shim answers tools/list from what it knows and forwards tools/call
// untouched, so the agent's toolset stops depending on a race it cannot see.

// toolCacheName is where a genuinely advertised tool list is remembered. The
// launcher hands over full schemas only when it is the host; caching them means
// one good connection teaches every later run, including the ones that come up
// secondary.
const toolCacheName = "studio-tools.json"

// ShimOptions configures Serve. Dial is a seam for tests; it defaults to the
// real stdio launcher transport.
type ShimOptions struct {
	Launch    LaunchConfig
	CachePath string
	Dial      Dialer
	// QuestionsOnly serves StudioForge's own single-tool server instead of the
	// Studio passthrough: it advertises the operator question and nothing else,
	// and never opens a launcher connection. This is the server every Claude run
	// gets, whether or not it was granted Studio.
	QuestionsOnly bool
}

type shim struct {
	opts ShimOptions

	mu        sync.Mutex
	transport Transport
	tools     []Tool
}

// Serve runs the MCP stdio protocol against in/out until in is exhausted. It
// owns a launcher connection, opened lazily so that a client which only
// handshakes never spawns Studio machinery.
func Serve(ctx context.Context, in io.Reader, out io.Writer, opts ShimOptions) error {
	s := &shim{opts: opts}
	defer s.close()

	var writeMu sync.Mutex
	write := func(v any) error {
		body, err := json.Marshal(v)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err = out.Write(append(body, '\n'))
		return err
	}

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			continue // Not protocol traffic; the launcher logs on stderr, not here.
		}
		// A request without an id is a notification: act on it, answer nothing.
		if len(req.ID) == 0 {
			continue
		}
		result, rpcErr := s.dispatch(ctx, req.Method, req.Params)
		if rpcErr != nil {
			if err := write(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]any{"code": rpcErr.Code, "message": rpcErr.Message},
			}); err != nil {
				return err
			}
			continue
		}
		if err := write(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *shim) dispatch(ctx context.Context, method string, params json.RawMessage) (any, *Error) {
	name := ServerName
	if s.opts.QuestionsOnly {
		name = QuestionServerName
	}
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": name, "version": "1.0.0"},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		if s.opts.QuestionsOnly {
			return map[string]any{"tools": []Tool{questionTool()}}, nil
		}
		return map[string]any{"tools": s.toolList(ctx)}, nil
	case "tools/call":
		if s.opts.QuestionsOnly {
			return s.callQuestion(params)
		}
		return s.callTool(ctx, params)
	default:
		return nil, &Error{Code: codeMethodNotFound, Message: "unknown method " + method}
	}
}

// toolList resolves the tools to advertise, best available first: what the
// launcher reports now, then what it reported on some earlier run, and finally
// the known tool names with an open schema. The last case still lets an agent
// call Studio — the call is what matters — but without argument hints, so it is
// a floor rather than a goal.
func (s *shim) toolList(ctx context.Context) []Tool {
	s.mu.Lock()
	if len(s.tools) > 0 {
		defer s.mu.Unlock()
		return s.tools
	}
	s.mu.Unlock()

	if transport, err := s.connect(ctx); err == nil {
		if live, err := transport.ListTools(ctx); err == nil && len(live) > 0 {
			s.saveCache(live)
			s.mu.Lock()
			s.tools = live
			s.mu.Unlock()
			return live
		}
	}
	tools := s.loadCache()
	if len(tools) == 0 {
		tools = fallbackTools()
	}
	s.mu.Lock()
	s.tools = tools
	s.mu.Unlock()
	return tools
}

// questionTool is the single tool the questions-only server advertises.
func questionTool() Tool {
	return Tool{Name: questions.ToolName, Description: questions.Description, InputSchema: questions.SchemaMap()}
}

// callQuestion answers the operator question entirely inside the shim.
//
// Nothing is sent anywhere from here, and nothing needs to be. The shim is a
// separate process that is never told which run it serves and has no route back
// to the daemon — which is exactly why Claude was left on the text fence in the
// first place. It does not need one: StudioForge already reads the CLI's own
// stream-json output, and a tool call appears in that stream with its arguments
// attached. Validating here and letting the call be observed there is what turns
// the question into a schema-checked tool without inventing a transport.
func (s *shim) callQuestion(params json.RawMessage) (any, *Error) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, &Error{Code: -32602, Message: "invalid tools/call params: " + err.Error()}
	}
	if call.Name != questions.ToolName {
		return nil, &Error{Code: codeMethodNotFound, Message: "unknown tool " + call.Name}
	}
	var q questions.Question
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &q); err != nil {
			return toolError("invalid arguments: " + err.Error()), nil
		}
	}
	validated, err := q.Validate()
	if err != nil {
		// A contract violation is returned as a tool error rather than a protocol
		// error: the agent can read it, fix the arguments and call again, which is
		// the entire advantage this has over a malformed fence that simply never
		// rendered.
		return toolError(err.Error()), nil
	}
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": questions.Accepted(validated)}}}, nil
}

// toolError is an MCP tool result the model is meant to read and act on, as
// opposed to a JSON-RPC error, which it is not.
func toolError(message string) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []any{map[string]any{"type": "text", "text": message}},
	}
}

func (s *shim) callTool(ctx context.Context, params json.RawMessage) (any, *Error) {
	var call struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, &Error{Code: -32602, Message: "invalid tools/call params: " + err.Error()}
	}
	if call.Name == "" {
		return nil, &Error{Code: -32602, Message: "tools/call requires a tool name"}
	}
	transport, err := s.connect(ctx)
	if err != nil {
		return nil, &Error{Code: -32603, Message: err.Error()}
	}
	raw, err := transport.Call(ctx, call.Name, call.Arguments)
	if err != nil {
		var rpc *Error
		if errors.As(err, &rpc) {
			return nil, rpc
		}
		return nil, &Error{Code: -32603, Message: err.Error()}
	}
	// The launcher's result is already a well-formed MCP tool result; passing it
	// through verbatim keeps content types the shim does not model intact.
	return json.RawMessage(raw), nil
}

func (s *shim) connect(ctx context.Context) (Transport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transport != nil {
		return s.transport, nil
	}
	dial := s.opts.Dial
	if dial == nil {
		dial = func(ctx context.Context, launch LaunchConfig) (Transport, error) {
			return NewStdioTransport(ctx, launch)
		}
	}
	transport, err := dial(ctx, s.opts.Launch)
	if err != nil {
		return nil, fmt.Errorf("open Studio MCP launcher: %w", err)
	}
	s.transport = transport
	return transport, nil
}

func (s *shim) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transport != nil {
		_ = s.transport.Close()
		s.transport = nil
	}
}

func (s *shim) loadCache() []Tool {
	if s.opts.CachePath == "" {
		return nil
	}
	body, err := os.ReadFile(s.opts.CachePath)
	if err != nil {
		return nil
	}
	var tools []Tool
	if json.Unmarshal(body, &tools) != nil {
		return nil
	}
	return tools
}

func (s *shim) saveCache(tools []Tool) {
	if s.opts.CachePath == "" {
		return
	}
	body, err := json.Marshal(tools)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(s.opts.CachePath), 0o700) != nil {
		return
	}
	// A stale cache is better than a failed run, so a write failure is ignored.
	_ = os.WriteFile(s.opts.CachePath, body, 0o600)
}

// fallbackTools describes the known Studio tools when no schema is available
// from any source. The open schema is deliberate: the agent may not know an
// argument's name, but it can still reach Studio and read the error it gets
// back, which beats being told Studio has no tools at all.
func fallbackTools() []Tool {
	tools := make([]Tool, 0, len(OfficialTools))
	for _, name := range OfficialTools {
		tools = append(tools, Tool{
			Name:        name,
			Description: "Roblox Studio tool " + name + ". Argument schema unavailable: this Studio did not publish one, so pass the arguments the tool documents and read the error if they are wrong.",
			InputSchema: map[string]any{"type": "object", "additionalProperties": true},
		})
	}
	return tools
}
