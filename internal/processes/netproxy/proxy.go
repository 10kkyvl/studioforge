package netproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const policyName = "registry-only"

type Allowlist struct {
	exact     map[string]struct{}
	wildcards []string
}

func ParseAllowlist(raw string) (Allowlist, error) {
	list := Allowlist{exact: map[string]struct{}{}}
	pieces := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' })
	for _, piece := range pieces {
		entry := strings.TrimSpace(piece)
		if entry == "" {
			continue
		}
		if err := list.add(entry); err != nil {
			return Allowlist{}, err
		}
	}
	return list, nil
}

func (a *Allowlist) add(entry string) error {
	if entry == "*" {
		return fmt.Errorf("netproxy: allowlist entry %q must not be a bare wildcard", entry)
	}
	if strings.Contains(entry, "://") {
		return fmt.Errorf("netproxy: allowlist entry %q must not include a scheme", entry)
	}
	if strings.ContainsAny(entry, " \t") {
		return fmt.Errorf("netproxy: allowlist entry %q must not contain whitespace", entry)
	}
	if strings.Contains(entry, "/") {
		return fmt.Errorf("netproxy: allowlist entry %q must not include a path", entry)
	}
	if strings.Contains(entry, ":") {
		return fmt.Errorf("netproxy: allowlist entry %q must not include a port", entry)
	}
	lower := strings.ToLower(entry)
	if strings.HasPrefix(lower, "*.") {
		domain := lower[2:]
		if domain == "" || strings.Contains(domain, "*") {
			return fmt.Errorf("netproxy: allowlist entry %q has an empty wildcard pattern", entry)
		}
		if a.exact == nil {
			a.exact = map[string]struct{}{}
		}
		a.wildcards = append(a.wildcards, domain)
		return nil
	}
	if strings.Contains(lower, "*") {
		return fmt.Errorf("netproxy: allowlist entry %q may only use a leading \"*.\" wildcard", entry)
	}
	if a.exact == nil {
		a.exact = map[string]struct{}{}
	}
	a.exact[lower] = struct{}{}
	return nil
}

func DefaultAllowlist() Allowlist {
	raw := strings.Join([]string{
		"registry.npmjs.org",
		"registry.yarnpkg.com",
		"proxy.golang.org",
		"sum.golang.org",
		"index.crates.io",
		"static.crates.io",
		"crates.io",
		"pypi.org",
		"files.pythonhosted.org",
		"github.com",
		"codeload.github.com",
		"objects.githubusercontent.com",
		"raw.githubusercontent.com",
	}, "\n")
	list, err := ParseAllowlist(raw)
	if err != nil {
		panic("netproxy: default allowlist failed to parse: " + err.Error())
	}
	return list
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	return host
}

func (a Allowlist) Permits(host string) bool {
	host = normalizeHost(host)
	if host == "" {
		return false
	}
	if _, ok := a.exact[host]; ok {
		return true
	}
	for _, domain := range a.wildcards {
		if strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (a Allowlist) Hosts() []string {
	out := make([]string, 0, len(a.exact)+len(a.wildcards))
	for host := range a.exact {
		out = append(out, host)
	}
	for _, domain := range a.wildcards {
		out = append(out, "*."+domain)
	}
	sort.Strings(out)
	return out
}

type Options struct {
	Allow     Allowlist
	OnAttempt func(host string, allowed bool)
	Logger    *slog.Logger
}

type Proxy struct {
	ln           net.Listener
	opts         Options
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	ctxWG        sync.WaitGroup
	mu           sync.Mutex
	conns        map[net.Conn]struct{}
	closed       bool
	shutdownOnce sync.Once
}

func Start(ctx context.Context, opts Options) (*Proxy, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("netproxy: listen on loopback: %w", err)
	}
	pctx, cancel := context.WithCancel(ctx)
	p := &Proxy{
		ln:     ln,
		opts:   opts,
		cancel: cancel,
		conns:  map[net.Conn]struct{}{},
	}
	p.wg.Add(1)
	go p.acceptLoop()
	p.ctxWG.Add(1)
	go func() {
		defer p.ctxWG.Done()
		<-pctx.Done()
		p.shutdown()
	}()
	return p, nil
}

func (p *Proxy) Addr() string {
	return p.ln.Addr().String()
}

func (p *Proxy) logger() *slog.Logger {
	if p.opts.Logger != nil {
		return p.opts.Logger
	}
	return slog.Default()
}

func (p *Proxy) addConn(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.conns[c] = struct{}{}
	return true
}

func (p *Proxy) removeConn(c net.Conn) {
	p.mu.Lock()
	delete(p.conns, c)
	p.mu.Unlock()
}

func (p *Proxy) shutdown() {
	p.shutdownOnce.Do(func() {
		p.cancel()
		_ = p.ln.Close()
		p.mu.Lock()
		p.closed = true
		conns := make([]net.Conn, 0, len(p.conns))
		for c := range p.conns {
			conns = append(conns, c)
		}
		p.mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
}

func (p *Proxy) Close() error {
	p.shutdown()
	p.wg.Wait()
	p.ctxWG.Wait()
	return nil
}

func (p *Proxy) acceptLoop() {
	defer p.wg.Done()
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return
		}
		if !p.addConn(conn) {
			_ = conn.Close()
			continue
		}
		p.wg.Add(1)
		go p.handle(conn)
	}
}

func (p *Proxy) handle(conn net.Conn) {
	defer p.wg.Done()
	defer p.removeConn(conn)
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	if req.Method != http.MethodConnect {
		p.respond(conn, http.StatusMethodNotAllowed, "only the CONNECT method is supported by this proxy")
		return
	}

	host := req.URL.Hostname()
	portStr := req.URL.Port()
	port, err := strconv.Atoi(portStr)
	if err != nil || (port != 443 && port != 80) {
		p.reportAttempt(host, false)
		p.respond(conn, http.StatusForbidden, fmt.Sprintf(
			"network policy %q denied %s:%s: only ports 443 and 80 are permitted for CONNECT",
			policyName, host, portStr))
		return
	}

	if !p.opts.Allow.Permits(host) {
		p.reportAttempt(host, false)
		p.respond(conn, http.StatusForbidden, fmt.Sprintf(
			"network policy %q denied %s: not in the allowed registry endpoint list",
			policyName, host))
		return
	}
	p.reportAttempt(host, true)

	target, err := net.DialTimeout("tcp", net.JoinHostPort(host, portStr), 10*time.Second)
	if err != nil {
		p.respond(conn, http.StatusBadGateway, fmt.Sprintf("could not connect to %s: %v", host, err))
		return
	}
	if !p.addConn(target) {
		_ = target.Close()
		return
	}
	defer p.removeConn(target)
	defer target.Close()

	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		return
	}

	var copyWG sync.WaitGroup
	copyWG.Add(2)
	p.wg.Add(2)
	go func() {
		defer copyWG.Done()
		defer p.wg.Done()
		_, _ = io.Copy(target, reader)
		_ = target.Close()
	}()
	go func() {
		defer copyWG.Done()
		defer p.wg.Done()
		_, _ = io.Copy(conn, target)
		_ = conn.Close()
	}()
	copyWG.Wait()
}

func (p *Proxy) respond(conn net.Conn, status int, message string) {
	body := message + "\n"
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	response := fmt.Sprintf(
		"HTTP/1.1 %d %s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		status, http.StatusText(status), len(body), body)
	if _, err := io.WriteString(conn, response); err != nil {
		p.logger().Debug("netproxy: failed to write response", "error", err)
	}
}

func (p *Proxy) reportAttempt(host string, allowed bool) {
	cb := p.opts.OnAttempt
	if cb == nil {
		return
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer func() { _ = recover() }()
		cb(host, allowed)
	}()
}
