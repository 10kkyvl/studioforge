//go:build darwin

package processes

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// registryProxy is the only network endpoint visible to a macOS registry-only
// child. The sandbox permits the loopback listener, while this proxy performs
// the hostname/port check before resolving or dialing the destination.
type registryProxy struct {
	listener net.Listener
	done     chan struct{}
	wg       sync.WaitGroup
	hosts    map[string]struct{}
	once     sync.Once
	mu       sync.Mutex
	logs     []NetworkObservation
	conns    map[net.Conn]struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	dial     func(context.Context, string, string) (net.Conn, error)
}

func newRegistryProxy(hosts []string) (*registryProxy, error) {
	allowed := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		host = normalizeRegistryHost(host)
		if err := validateRegistryHost(host); err != nil {
			return nil, err
		}
		allowed[host] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("registry-only network policy requires at least one valid registry host")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start registry proxy: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &registryProxy{listener: listener, done: make(chan struct{}), hosts: allowed, conns: map[net.Conn]struct{}{}, ctx: ctx, cancel: cancel}
	p.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	p.wg.Add(1)
	go p.serve()
	return p, nil
}

func (p *registryProxy) Port() int {
	_, port, _ := net.SplitHostPort(p.listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return n
}

func (p *registryProxy) URL() string { return "http://127.0.0.1:" + strconv.Itoa(p.Port()) }

func (p *registryProxy) serve() {
	defer p.wg.Done()
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.done:
				return
			default:
			}
			continue
		}
		p.mu.Lock()
		select {
		case <-p.done:
			p.mu.Unlock()
			_ = conn.Close()
			return
		default:
		}
		p.conns[conn] = struct{}{}
		p.mu.Unlock()
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.handle(conn)
		}()
	}
}

func (p *registryProxy) handle(conn net.Conn) {
	defer conn.Close()
	defer p.removeConn(conn)
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	limited := &limitedReader{r: conn, remaining: 64 * 1024}
	reader := bufio.NewReader(limited)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	limited.remaining = -1
	host, port := requestAuthority(req)
	allowed := req.Method == http.MethodConnect && p.allowed(host, port)
	p.record(NetworkObservation{Host: host, Port: port, Allowed: allowed})
	if !allowed {
		_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
		return
	}
	if req.Method == http.MethodConnect {
		p.tunnel(conn, reader, host, port)
		return
	}
	// Only CONNECT host:443 is supported. HTTP proxy requests would add a
	// second request format and make it easier to bypass the authority check.
}

func (p *registryProxy) tunnel(client net.Conn, buffered io.Reader, host, port string) {
	target, err := p.dial(p.ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		_, _ = io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer target.Close()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	_ = client.SetDeadline(time.Time{})
	_ = target.SetDeadline(time.Time{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(target, buffered)
		_ = target.Close()
		_ = client.Close()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, target)
		_ = target.Close()
		_ = client.Close()
	}()
	wg.Wait()
}

func requestAuthority(req *http.Request) (string, string) {
	authority := req.URL.Host
	if authority == "" {
		authority = req.Host
	}
	host, port, err := net.SplitHostPort(authority)
	if err != nil {
		host = strings.TrimSpace(authority)
		port = "443"
		if req.URL.Scheme == "http" {
			port = "80"
		}
	}
	return normalizeRegistryHost(host), port
}

func (p *registryProxy) allowed(host, port string) bool {
	if port != "443" {
		return false
	}
	_, ok := p.hosts[host]
	return ok
}

func (p *registryProxy) record(observation NetworkObservation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.logs) < 128 {
		p.logs = append(p.logs, observation)
	}
}

func (p *registryProxy) removeConn(conn net.Conn) {
	p.mu.Lock()
	delete(p.conns, conn)
	p.mu.Unlock()
}

func (p *registryProxy) Observations() []NetworkObservation {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]NetworkObservation(nil), p.logs...)
}

func (p *registryProxy) Close() {
	p.once.Do(func() {
		close(p.done)
		p.cancel()
		_ = p.listener.Close()
		p.mu.Lock()
		conns := make([]net.Conn, 0, len(p.conns))
		for conn := range p.conns {
			conns = append(conns, conn)
		}
		p.mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	})
	p.wg.Wait()
}

type limitedReader struct {
	r         io.Reader
	remaining int64
}

func (r *limitedReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, fmt.Errorf("proxy request headers exceed 64 KiB")
	}
	if r.remaining > 0 && int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func normalizeRegistryHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

func validateRegistryHost(host string) error {
	if host == "" || strings.ContainsAny(host, "/\\:*?#[\t\r\n ") || net.ParseIP(host) != nil {
		return fmt.Errorf("invalid registry host %q", host)
	}
	return nil
}

func withProxyEnvironment(env []string, proxyURL string) []string {
	managed := map[string]bool{
		"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
	}
	out := make([]string, 0, len(env)+8)
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && managed[strings.ToUpper(key)] {
			continue
		}
		out = append(out, entry)
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		out = append(out, key+"="+proxyURL)
	}
	out = append(out, "NO_PROXY=", "no_proxy=")
	return out
}
