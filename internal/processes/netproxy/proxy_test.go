package netproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAllowlistMatchesExactHostsAndWildcardSuffixes(t *testing.T) {
	list, err := ParseAllowlist("registry.npmjs.org\n*.githubusercontent.com")
	if err != nil {
		t.Fatal(err)
	}
	if !list.Permits("registry.npmjs.org") {
		t.Fatal("expected the exact host to be permitted")
	}
	if !list.Permits("objects.githubusercontent.com") {
		t.Fatal("expected a subdomain of the wildcard pattern to be permitted")
	}
	if list.Permits("registry.yarnpkg.com") {
		t.Fatal("expected a host outside the list to be denied")
	}
}

func TestAllowlistWildcardDoesNotMatchTheBareDomain(t *testing.T) {
	list, err := ParseAllowlist("*.githubusercontent.com")
	if err != nil {
		t.Fatal(err)
	}
	if list.Permits("githubusercontent.com") {
		t.Fatal("a *.example.com pattern must not match the bare example.com domain, only its subdomains")
	}
	if !list.Permits("raw.githubusercontent.com") {
		t.Fatal("expected a genuine subdomain to be permitted")
	}
}

func TestParseAllowlistRefusesSchemesPathsAndPorts(t *testing.T) {
	cases := []string{
		"https://registry.npmjs.org",
		"registry.npmjs.org/path",
		"registry.npmjs.org:443",
		"registry npmjs org",
		"*.",
		"*",
	}
	for _, entry := range cases {
		if _, err := ParseAllowlist(entry); err == nil {
			t.Fatalf("ParseAllowlist(%q) = nil error, want a refusal", entry)
		}
	}
}

func TestParseAllowlistAcceptsCommasAndNewlines(t *testing.T) {
	list, err := ParseAllowlist("a.example.com, b.example.com\nc.example.com,\n\n  d.example.com  ")
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"a.example.com", "b.example.com", "c.example.com", "d.example.com"} {
		if !list.Permits(host) {
			t.Fatalf("expected %s to be permitted, hosts=%v", host, list.Hosts())
		}
	}
}

func TestAllowlistMatchingIsCaseInsensitive(t *testing.T) {
	list, err := ParseAllowlist("Registry.NPMJS.org")
	if err != nil {
		t.Fatal(err)
	}
	if !list.Permits("registry.npmjs.org") {
		t.Fatal("expected a lowercase host to match an uppercase pattern")
	}
	if !list.Permits("REGISTRY.NPMJS.ORG.") {
		t.Fatal("expected an uppercase host with a trailing FQDN dot to match")
	}
}

func startProxy(t *testing.T, opts Options) *Proxy {
	t.Helper()
	p, err := Start(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func sendRawRequest(t *testing.T, addr, request string) (statusLine string, body string, conn net.Conn) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	statusLine, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		lines = append(lines, line)
	}
	rest := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	n, _ := reader.Read(rest)
	body = string(rest[:n])
	return statusLine, body, conn
}

func TestProxyListensOnLoopbackOnly(t *testing.T) {
	p := startProxy(t, Options{Allow: DefaultAllowlist()})
	host, _, err := net.SplitHostPort(p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("proxy listened on %q, want it bound to 127.0.0.1 only", host)
	}
}

func listenOnAPrivilegedPort(t *testing.T) net.Listener {
	t.Helper()
	for _, port := range []int{80, 443} {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return ln
		}
	}
	t.Skip("no privileged port (80 or 443) could be bound in this environment; skipping the end-to-end tunnel test")
	return nil
}

func TestProxyTunnelsAnAllowedHost(t *testing.T) {
	echo := listenOnAPrivilegedPort(t)
	defer echo.Close()
	_, echoPort, err := net.SplitHostPort(echo.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := echo.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						if _, werr := c.Write(buf[:n]); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	allow, err := ParseAllowlist("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	p := startProxy(t, Options{Allow: allow})

	conn, err := net.DialTimeout("tcp", p.Addr(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	target := "127.0.0.1:" + echoPort
	if _, err := io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statusLine, "200") {
		t.Fatalf("status line = %q, want 200 Connection established", statusLine)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	payload := "hello through the tunnel"
	if _, err := io.WriteString(conn, payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("echoed payload = %q, want %q", got, payload)
	}
}

func TestProxyRefusesADeniedHostWithAPolicyMessage(t *testing.T) {
	allow, err := ParseAllowlist("registry.npmjs.org")
	if err != nil {
		t.Fatal(err)
	}
	p := startProxy(t, Options{Allow: allow})
	statusLine, body, conn := sendRawRequest(t, p.Addr(),
		"CONNECT blocked.example.invalid:443 HTTP/1.1\r\nHost: blocked.example.invalid:443\r\n\r\n")
	defer conn.Close()
	if !strings.Contains(statusLine, "403") {
		t.Fatalf("status line = %q, want 403 Forbidden", statusLine)
	}
	if !strings.Contains(body, policyName) || !strings.Contains(body, "blocked.example.invalid") {
		t.Fatalf("body = %q, want it to name the policy and the denied host", body)
	}
}

func TestProxyRefusesNonConnectMethods(t *testing.T) {
	p := startProxy(t, Options{Allow: DefaultAllowlist()})
	statusLine, _, conn := sendRawRequest(t, p.Addr(),
		"GET http://registry.npmjs.org/ HTTP/1.1\r\nHost: registry.npmjs.org\r\n\r\n")
	defer conn.Close()
	if !strings.Contains(statusLine, "405") {
		t.Fatalf("status line = %q, want 405 Method Not Allowed", statusLine)
	}
}

func TestProxyRefusesANonStandardPort(t *testing.T) {
	p := startProxy(t, Options{Allow: DefaultAllowlist()})
	statusLine, body, conn := sendRawRequest(t, p.Addr(),
		"CONNECT registry.npmjs.org:8080 HTTP/1.1\r\nHost: registry.npmjs.org:8080\r\n\r\n")
	defer conn.Close()
	if !strings.Contains(statusLine, "403") {
		t.Fatalf("status line = %q, want a refusal for a non-standard port", statusLine)
	}
	if !strings.Contains(body, "port") {
		t.Fatalf("body = %q, want it to explain the port was rejected", body)
	}
}

func TestProxyReportsEveryAttemptThroughOnAttempt(t *testing.T) {
	var mu sync.Mutex
	type attempt struct {
		host    string
		allowed bool
	}
	var attempts []attempt

	allow, err := ParseAllowlist("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	p := startProxy(t, Options{Allow: allow, OnAttempt: func(host string, allowed bool) {
		mu.Lock()
		attempts = append(attempts, attempt{host, allowed})
		mu.Unlock()
	}})

	_, _, allowedConn := sendRawRequest(t, p.Addr(), "CONNECT 127.0.0.1:443 HTTP/1.1\r\nHost: 127.0.0.1:443\r\n\r\n")
	defer allowedConn.Close()
	_, _, deniedConn := sendRawRequest(t, p.Addr(), "CONNECT blocked.example.invalid:443 HTTP/1.1\r\nHost: blocked.example.invalid:443\r\n\r\n")
	defer deniedConn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := len(attempts)
		mu.Unlock()
		if got >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only observed %d attempts, want 2", got)
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	var sawAllowed, sawDenied bool
	for _, a := range attempts {
		if a.host == "127.0.0.1" && a.allowed {
			sawAllowed = true
		}
		if a.host == "blocked.example.invalid" && !a.allowed {
			sawDenied = true
		}
	}
	if !sawAllowed {
		t.Fatalf("expected an allowed attempt for 127.0.0.1, got %+v", attempts)
	}
	if !sawDenied {
		t.Fatalf("expected a denied attempt for blocked.example.invalid, got %+v", attempts)
	}
}

func TestProxyCloseIsIdempotentAndStopsListening(t *testing.T) {
	p, err := Start(context.Background(), Options{Allow: DefaultAllowlist()})
	if err != nil {
		t.Fatal(err)
	}
	addr := p.Addr()
	if err := p.Close(); err != nil {
		t.Fatalf("first Close returned %v, want nil", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close returned %v, want nil", err)
	}
	if _, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		t.Fatal("expected dialing the proxy after Close to fail")
	}
}

func TestProxyDoesNotLeakGoroutines(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()

	allow, err := ParseAllowlist("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Start(context.Background(), Options{Allow: allow, OnAttempt: func(string, bool) {}})
	if err != nil {
		t.Fatal(err)
	}

	_, _, allowedConn := sendRawRequest(t, p.Addr(), "CONNECT 127.0.0.1:443 HTTP/1.1\r\nHost: 127.0.0.1:443\r\n\r\n")
	allowedConn.Close()
	_, _, deniedConn := sendRawRequest(t, p.Addr(), "CONNECT blocked.example.invalid:443 HTTP/1.1\r\nHost: blocked.example.invalid:443\r\n\r\n")
	deniedConn.Close()

	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		runtime.GC()
		after := runtime.NumGoroutine()
		if after <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine count did not return to baseline: before=%d after=%d", before, after)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
