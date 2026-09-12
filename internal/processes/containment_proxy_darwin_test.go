//go:build darwin

package processes

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRegistryProxyCloseClosesIdleConnectionOnDarwin(t *testing.T) {
	proxy, err := newRegistryProxy([]string{"registry.npmjs.org"})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", proxy.listener.Addr().String())
	if err != nil {
		proxy.Close()
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		proxy.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		_ = conn.Close()
		t.Fatal("registry proxy close blocked on an idle client")
	}
	_ = conn.Close()
}

func TestRegistryProxyTunnelsLargePayloadAndClosesActiveTunnelOnDarwin(t *testing.T) {
	proxy, err := newRegistryProxy([]string{"registry.npmjs.org"})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	targetListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer targetListener.Close()
	targetAddress := targetListener.Addr().String()
	proxy.dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, targetAddress)
	}
	const payloadSize = 256 * 1024
	readDone := make(chan error, 1)
	go func() {
		conn, acceptErr := targetListener.Accept()
		if acceptErr != nil {
			readDone <- acceptErr
			return
		}
		defer conn.Close()
		_, readErr := io.CopyN(io.Discard, conn, payloadSize)
		readDone <- readErr
	}()
	client, err := net.Dial("tcp", proxy.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := io.WriteString(client, "CONNECT registry.npmjs.org:443 HTTP/1.1\r\nHost: registry.npmjs.org:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT response = %s", response.Status)
	}
	payload := bytes.Repeat([]byte("x"), payloadSize)
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("large CONNECT payload did not reach the target")
	}
	closed := make(chan struct{})
	go func() {
		proxy.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("registry proxy close blocked on an active tunnel")
	}
	if !strings.Contains(proxy.Observations()[0].Host, "registry.npmjs.org") {
		t.Fatalf("unexpected observation: %#v", proxy.Observations())
	}
}
