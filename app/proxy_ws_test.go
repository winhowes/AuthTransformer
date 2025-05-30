package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func wsAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func TestProxyHandlerWebSocket(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			t.Fatalf("expected websocket upgrade, got %q", r.Header.Get("Upgrade"))
		}
		hij, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("hijacking not supported")
		}
		conn, buf, err := hij.Hijack()
		if err != nil {
			t.Fatalf("hijack failed: %v", err)
		}
		defer conn.Close()
		accept := wsAccept(r.Header.Get("Sec-WebSocket-Key"))
		fmt.Fprintf(buf, "HTTP/1.1 101 Switching Protocols\r\n")
		fmt.Fprintf(buf, "Upgrade: websocket\r\n")
		fmt.Fprintf(buf, "Connection: Upgrade\r\n")
		fmt.Fprintf(buf, "Sec-WebSocket-Accept: %s\r\n\r\n", accept)
		buf.Flush()
	}))
	defer upstream.Close()

	integ := Integration{Name: "wsint", Destination: upstream.URL, InRateLimit: 1, OutRateLimit: 1}
	if err := AddIntegration(&integ); err != nil {
		t.Fatalf("add integration: %v", err)
	}
	t.Cleanup(func() { integ.inLimiter.Stop(); integ.outLimiter.Stop() })

	proxy := httptest.NewServer(http.HandlerFunc(proxyHandler))
	defer proxy.Close()

	conn, err := net.Dial("tcp", proxy.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "GET / HTTP/1.1\r\n")
	fmt.Fprintf(conn, "Host: wsint\r\n")
	fmt.Fprintf(conn, "Upgrade: websocket\r\n")
	fmt.Fprintf(conn, "Connection: Upgrade\r\n")
	fmt.Fprintf(conn, "Sec-WebSocket-Key: testkey==\r\n")
	fmt.Fprintf(conn, "Sec-WebSocket-Version: 13\r\n\r\n")

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}
}

func TestProxyHandlerWebSocketEcho(t *testing.T) {
	message := []byte{0x81, 0x05, 'h', 'e', 'l', 'l', 'o'}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			t.Fatalf("expected websocket upgrade, got %q", r.Header.Get("Upgrade"))
		}
		hij, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("hijacking not supported")
		}
		conn, buf, err := hij.Hijack()
		if err != nil {
			t.Fatalf("hijack failed: %v", err)
		}
		defer conn.Close()
		accept := wsAccept(r.Header.Get("Sec-WebSocket-Key"))
		fmt.Fprintf(buf, "HTTP/1.1 101 Switching Protocols\r\n")
		fmt.Fprintf(buf, "Upgrade: websocket\r\n")
		fmt.Fprintf(buf, "Connection: Upgrade\r\n")
		fmt.Fprintf(buf, "Sec-WebSocket-Accept: %s\r\n\r\n", accept)
		buf.Flush()

		echo := make([]byte, len(message))
		if _, err := io.ReadFull(conn, echo); err != nil {
			t.Fatalf("read message: %v", err)
		}
		if !bytes.Equal(echo, message) {
			t.Fatalf("server received %v, want %v", echo, message)
		}
		if _, err := conn.Write(echo); err != nil {
			t.Fatalf("write echo: %v", err)
		}
	}))
	defer upstream.Close()

	integ := Integration{Name: "wsecho", Destination: upstream.URL, InRateLimit: 1, OutRateLimit: 1}
	if err := AddIntegration(&integ); err != nil {
		t.Fatalf("add integration: %v", err)
	}
	t.Cleanup(func() { integ.inLimiter.Stop(); integ.outLimiter.Stop() })

	proxy := httptest.NewServer(http.HandlerFunc(proxyHandler))
	defer proxy.Close()

	conn, err := net.Dial("tcp", proxy.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "GET / HTTP/1.1\r\n")
	fmt.Fprintf(conn, "Host: wsecho\r\n")
	fmt.Fprintf(conn, "Upgrade: websocket\r\n")
	fmt.Fprintf(conn, "Connection: Upgrade\r\n")
	fmt.Fprintf(conn, "Sec-WebSocket-Key: testkey==\r\n")
	fmt.Fprintf(conn, "Sec-WebSocket-Version: 13\r\n\r\n")

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}

	if _, err := conn.Write(message); err != nil {
		t.Fatalf("write message: %v", err)
	}

	reply := make([]byte, len(message))
	if _, err := io.ReadFull(br, reply); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if !bytes.Equal(reply, message) {
		t.Fatalf("expected echo %v, got %v", message, reply)
	}
}
