package transport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func startEchoWebSocket(t *testing.T, cfg WebSocketConfig) (string, <-chan error) {
	t.Helper()
	failures := make(chan error, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, _, hijacked, err := AcceptWebSocket(w, r, cfg)
		if err != nil {
			if !hijacked {
				writeTransportProblem(w, http.StatusBadRequest, "UPGRADE_REJECTED")
			}
			return
		}
		defer ws.Close()
		for {
			binary, raw, err := ws.Read(r.Context())
			if err != nil {
				failures <- err
				return
			}
			if !binary {
				failures <- errors.New("unexpected text frame")
				return
			}
			if err := ws.Write(r.Context(), raw); err != nil {
				failures <- err
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return strings.TrimPrefix(server.URL, "http://"), failures
}

func TestWebSocketBinaryEchoAndLimits(t *testing.T) {
	address, failures := startEchoWebSocket(t, WebSocketConfig{MaxFrameBytes: 1024})
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, connectionID, err := ClientWebSocket(ctx, conn, address, "/ws", WebSocketConfig{MaxFrameBytes: 1024})
	if err != nil {
		t.Fatalf("ClientWebSocket: %v", err)
	}
	defer ws.Close()
	if connectionID == "" {
		t.Fatal("missing server connection ID")
	}
	if err := ws.Write(ctx, []byte("hello-agent")); err != nil {
		t.Fatalf("write: %v", err)
	}
	binary, raw, err := ws.Read(ctx)
	if err != nil || !binary || string(raw) != "hello-agent" {
		t.Fatalf("echo = %t %q %v", binary, raw, err)
	}
	if err := ws.Write(ctx, make([]byte, 1025)); !errors.Is(err, ErrWebSocketRejected) {
		t.Fatalf("oversized write = %v, want ErrWebSocketRejected", err)
	}
	_ = ws.Close()
	select {
	case err := <-failures:
		if err == nil || !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
			// The handler may observe EOF or a closed connection after Close.
			t.Logf("server read ended with %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not observe close")
	}
}

func rawUpgrade(t *testing.T, address string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(nonce[:])
	request := "GET /ws HTTP/1.1\r\nHost: " + address + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + "\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil || response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("raw upgrade = %v/%v", response, err)
	}
	return conn, reader
}

func writeRawFrame(conn net.Conn, fin bool, opcode byte, payload []byte, mask bool) error {
	first := opcode
	if fin {
		first |= 0x80
	}
	frame := []byte{first}
	flag := byte(0)
	if mask {
		flag = 0x80
	}
	switch {
	case len(payload) < 126:
		frame = append(frame, flag|byte(len(payload)))
	case len(payload) <= 0xffff:
		frame = append(frame, flag|126, byte(len(payload)>>8), byte(len(payload)))
	default:
		return errors.New("test payload too large")
	}
	if mask {
		var key [4]byte
		if _, err := rand.Read(key[:]); err != nil {
			return err
		}
		frame = append(frame, key[:]...)
		for i, b := range payload {
			frame = append(frame, b^key[i%4])
		}
	} else {
		frame = append(frame, payload...)
	}
	_, err := conn.Write(frame)
	return err
}

func TestWebSocketRejectsUnmaskedFragmentedAndTextFrames(t *testing.T) {
	cases := []struct {
		name    string
		frame   func(net.Conn) error
		wantErr bool
	}{
		{"unmasked", func(conn net.Conn) error { return writeRawFrame(conn, true, 0x2, []byte("x"), false) }, true},
		{"fragmented", func(conn net.Conn) error { return writeRawFrame(conn, false, 0x2, []byte("x"), true) }, true},
		{"continuation", func(conn net.Conn) error { return writeRawFrame(conn, true, 0x0, []byte("x"), true) }, true},
		{"text", func(conn net.Conn) error { return writeRawFrame(conn, true, 0x1, []byte("text"), true) }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			address, failures := startEchoWebSocket(t, WebSocketConfig{MaxFrameBytes: 1024})
			conn, _ := rawUpgrade(t, address)
			defer conn.Close()
			if err := tc.frame(conn); err != nil {
				t.Fatal(err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			select {
			case err := <-failures:
				if tc.wantErr && err == nil {
					t.Fatal("frame unexpectedly accepted")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("server did not reject the frame")
			}
		})
	}
}

func TestWebSocketUpgradeRejectsExtensions(t *testing.T) {
	cfg := WebSocketConfig{MaxFrameBytes: 1024}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, hijacked, err := AcceptWebSocket(w, r, cfg); !hijacked && err != nil {
			writeTransportProblem(w, http.StatusBadRequest, "UPGRADE_REJECTED")
			return
		}
	}))
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16)))
	request.Header.Set("Sec-WebSocket-Extensions", "permessage-deflate")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("extension upgrade status = %d", response.StatusCode)
	}
}
