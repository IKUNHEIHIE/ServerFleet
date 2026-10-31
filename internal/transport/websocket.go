package transport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// This file is a deliberately small RFC 6455 adapter for the reviewed
// ControllerLinks/AgentSession FrameSocket contract. It supports one binary
// message per frame with no extensions, no compression, no fragmentation and no
// subprotocol negotiation. It is not a general-purpose WebSocket server.

const webSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// ServerConnectionIDHeader is set by the trusted upgrade handler. The Agent must
// use this value as its connection ID; it is never taken from an inbound frame.
const ServerConnectionIDHeader = "Sec-Serverfleet-Connection-Id"

var ErrWebSocketRejected = errors.New("transport: websocket rejected")

// WebSocketConfig bounds every inbound and outbound frame before allocation.
type WebSocketConfig struct {
	MaxFrameBytes int
}

func (c WebSocketConfig) valid() bool {
	return c.MaxFrameBytes > 0 && c.MaxFrameBytes <= 1<<20
}

// WebSocketConn implements FrameSocket for one upgraded connection.
type WebSocketConn struct {
	conn   net.Conn
	reader *bufio.Reader
	client bool
	max    int

	readMu  sync.Mutex
	writeMu sync.Mutex

	closeOnce sync.Once
	closed    chan struct{}
	pong      chan []byte
}

// AcceptWebSocket validates the strict upgrade profile, hijacks the connection,
// writes the 101 response and returns the frame socket plus its connection ID.
// The hijacked result reports whether the HTTP response was already committed;
// callers must not write an error response when it is true.
func AcceptWebSocket(w http.ResponseWriter, r *http.Request, cfg WebSocketConfig) (*WebSocketConn, string, bool, error) {
	if w == nil || r == nil || !cfg.valid() {
		return nil, "", false, ErrWebSocketRejected
	}
	if r.Method != http.MethodGet || r.ProtoMajor != 1 || r.ProtoMinor < 1 {
		return nil, "", false, ErrWebSocketRejected
	}
	if !headerHasToken(r.Header, "Connection", "upgrade") || !strings.EqualFold(singleHeader(r.Header, "Upgrade"), "websocket") {
		return nil, "", false, ErrWebSocketRejected
	}
	if singleHeader(r.Header, "Sec-WebSocket-Version") != "13" {
		return nil, "", false, ErrWebSocketRejected
	}
	if len(r.Header.Values("Sec-WebSocket-Extensions")) != 0 || len(r.Header.Values("Sec-WebSocket-Protocol")) != 0 {
		return nil, "", false, ErrWebSocketRejected
	}
	keys := r.Header.Values("Sec-WebSocket-Key")
	if len(keys) != 1 {
		return nil, "", false, ErrWebSocketRejected
	}
	key, err := base64.StdEncoding.DecodeString(keys[0])
	if err != nil || len(key) != 16 || base64.StdEncoding.EncodeToString(key) != keys[0] {
		return nil, "", false, ErrWebSocketRejected
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		return nil, "", false, ErrWebSocketRejected
	}
	connectionID, err := NewConnectionID()
	if err != nil {
		return nil, "", false, ErrWebSocketRejected
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, "", false, ErrWebSocketRejected
	}
	conn, buffered, err := hijacker.Hijack()
	if err != nil {
		return nil, "", false, ErrWebSocketRejected
	}
	// A hijacked connection must not inherit the http.Server request deadlines;
	// long-lived WSS sessions manage their own bounded frame deadlines.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, "", true, ErrWebSocketRejected
	}
	acceptHash := sha1.Sum([]byte(keys[0] + webSocketGUID))
	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(acceptHash[:]) + "\r\n" +
		ServerConnectionIDHeader + ": " + connectionID + "\r\n\r\n"
	if _, err = buffered.WriteString(response); err == nil {
		err = buffered.Flush()
	}
	if err != nil {
		_ = conn.Close()
		return nil, "", true, ErrWebSocketRejected
	}
	return &WebSocketConn{conn: conn, reader: buffered.Reader, client: false, max: cfg.MaxFrameBytes, closed: make(chan struct{}), pong: make(chan []byte, 1)}, connectionID, true, nil
}

// ClientWebSocket performs the Agent side of the same strict profile over an
// already-established (mTLS) connection. The caller owns TLS and trust material.
func ClientWebSocket(ctx context.Context, conn net.Conn, host, path string, cfg WebSocketConfig) (*WebSocketConn, string, error) {
	if ctx == nil || conn == nil || host == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(host+path, "\r\n\x00") || !cfg.valid() {
		return nil, "", ErrWebSocketRejected
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, "", ErrWebSocketRejected
	}
	key := base64.StdEncoding.EncodeToString(nonce[:])
	deadline := time.Now().Add(10 * time.Second)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, "", ErrWebSocketRejected
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()
	request := "GET " + path + " HTTP/1.1\r\nHost: " + host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + "\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		return nil, "", ErrWebSocketRejected
	}
	reader := bufio.NewReaderSize(conn, 32<<10)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil || response.StatusCode != http.StatusSwitchingProtocols || !strings.EqualFold(response.Header.Get("Upgrade"), "websocket") || !headerHasToken(response.Header, "Connection", "upgrade") {
		return nil, "", ErrWebSocketRejected
	}
	acceptHash := sha1.Sum([]byte(key + webSocketGUID))
	if response.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(acceptHash[:]) || len(response.Header.Values("Sec-WebSocket-Extensions")) != 0 || len(response.Header.Values("Sec-WebSocket-Protocol")) != 0 {
		return nil, "", ErrWebSocketRejected
	}
	connectionID := response.Header.Get(ServerConnectionIDHeader)
	if !validBindingID(connectionID) || len(connectionID) > 128 {
		return nil, "", ErrWebSocketRejected
	}
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}
	return &WebSocketConn{conn: conn, reader: reader, client: true, max: cfg.MaxFrameBytes, closed: make(chan struct{}), pong: make(chan []byte, 1)}, connectionID, nil
}

func singleHeader(header http.Header, name string) string {
	values := header.Values(name)
	if len(values) != 1 {
		return ""
	}
	return values[0]
}

func headerHasToken(header http.Header, name, token string) bool {
	values := header.Values(name)
	if len(values) != 1 {
		return false
	}
	for _, part := range strings.Split(values[0], ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// Read returns exactly one binary or text frame. Text frames are reported with
// binary=false so callers may reject them; the RFC 6455 payload itself is
// validated as UTF-8. Control frames are handled internally.
func (c *WebSocketConn) Read(ctx context.Context) (bool, []byte, error) {
	if c == nil || ctx == nil {
		return false, nil, ErrWebSocketRejected
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return false, nil, err
		}
		opcode, payload, err := c.readFrame(ctx)
		if err != nil {
			return false, nil, err
		}
		switch opcode {
		case 0x2:
			return true, payload, nil
		case 0x1:
			if !utf8.Valid(payload) {
				return false, nil, ErrWebSocketRejected
			}
			return false, payload, nil
		case 0x8:
			_ = c.writeControl(ctx, 0x8, append([]byte{0x03, 0xe8}, nil...))
			c.closeSocket()
			return false, nil, io.EOF
		case 0x9:
			if err := c.writeControl(ctx, 0xa, payload); err != nil {
				return false, nil, err
			}
		case 0xa:
			select {
			case c.pong <- append([]byte(nil), payload...):
			default:
			}
			continue
		default:
			return false, nil, ErrWebSocketRejected
		}
	}
}

func (c *WebSocketConn) readFrame(ctx context.Context) (byte, []byte, error) {
	var header [2]byte
	if err := c.readFull(ctx, header[:]); err != nil {
		return 0, nil, err
	}
	fin := header[0]&0x80 != 0
	opcode := header[0] & 0x0f
	if header[0]&0x70 != 0 {
		return 0, nil, ErrWebSocketRejected
	}
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	control := opcode&0x08 != 0
	if control && (opcode != 0x8 && opcode != 0x9 && opcode != 0xa || !fin) {
		return 0, nil, ErrWebSocketRejected
	}
	if !control && (!fin || opcode != 0x1 && opcode != 0x2) {
		return 0, nil, ErrWebSocketRejected
	}
	if masked == c.client {
		return 0, nil, ErrWebSocketRejected
	}
	switch length {
	case 126:
		var extended [2]byte
		if err := c.readFull(ctx, extended[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(extended[:]))
		if length < 126 {
			return 0, nil, ErrWebSocketRejected
		}
	case 127:
		var extended [8]byte
		if err := c.readFull(ctx, extended[:]); err != nil {
			return 0, nil, err
		}
		if extended[0]&0x80 != 0 {
			return 0, nil, ErrWebSocketRejected
		}
		length = binary.BigEndian.Uint64(extended[:])
		if length <= 0xffff {
			return 0, nil, ErrWebSocketRejected
		}
	}
	if control && length > 125 || length > uint64(c.max) {
		return 0, nil, ErrWebSocketRejected
	}
	var mask [4]byte
	if masked {
		if err := c.readFull(ctx, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, int(length))
	if err := c.readFull(ctx, payload); err != nil {
		return 0, nil, err
	}
	if masked && len(payload) > 0 {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

func (c *WebSocketConn) readFull(ctx context.Context, buf []byte) error {
	restore, err := c.armDeadline(ctx, false)
	if err != nil {
		return err
	}
	defer restore()
	_, err = io.ReadFull(c.reader, buf)
	return err
}

func (c *WebSocketConn) Write(ctx context.Context, payload []byte) error {
	if c == nil || ctx == nil || len(payload) == 0 || len(payload) > c.max {
		return ErrWebSocketRejected
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(ctx, 0x2, payload)
}

// Ping sends one RFC 6455 ping and waits for the matching pong. A concurrent
// Read loop is required: it owns frame decoding and reports pongs here. The
// round trip is bounded by ctx so a dead peer cannot stall the caller.
func (c *WebSocketConn) Ping(ctx context.Context) error {
	if c == nil || ctx == nil || c.pong == nil {
		return ErrWebSocketRejected
	}
	select {
	case <-c.pong:
	default:
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ErrWebSocketRejected
	}
	if err := c.writeControl(ctx, 0x9, nonce[:]); err != nil {
		return err
	}
	for {
		select {
		case payload := <-c.pong:
			if bytes.Equal(payload, nonce[:]) {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-c.closed:
			return io.EOF
		}
	}
}

func (c *WebSocketConn) writeControl(ctx context.Context, opcode byte, payload []byte) error {
	if len(payload) > 125 {
		return ErrWebSocketRejected
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(ctx, opcode, payload)
}

func (c *WebSocketConn) writeFrame(ctx context.Context, opcode byte, payload []byte) error {
	restore, err := c.armDeadline(ctx, true)
	if err != nil {
		return err
	}
	defer restore()
	frame := make([]byte, 0, len(payload)+14)
	frame = append(frame, 0x80|opcode)
	mask := c.client
	length := len(payload)
	flag := byte(0)
	if mask {
		flag = 0x80
	}
	switch {
	case length < 126:
		frame = append(frame, flag|byte(length))
	case length <= 0xffff:
		frame = append(frame, flag|126, byte(length>>8), byte(length))
	default:
		frame = append(frame, flag|127, 0, 0, 0, 0, byte(length>>24), byte(length>>16), byte(length>>8), byte(length))
	}
	if mask {
		var key [4]byte
		if _, err := rand.Read(key[:]); err != nil {
			return ErrWebSocketRejected
		}
		frame = append(frame, key[:]...)
		for i, b := range payload {
			frame = append(frame, b^key[i%4])
		}
	} else {
		frame = append(frame, payload...)
	}
	_, err = c.conn.Write(frame)
	return err
}

// armDeadline installs a context-aware deadline and returns a restore function.
// The watcher is joined before the deadline is cleared so a late cancel cannot
// corrupt a subsequent read or write.
func (c *WebSocketConn) armDeadline(ctx context.Context, write bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	createDeadline := func(d time.Time) error {
		if write {
			return c.conn.SetWriteDeadline(d)
		}
		return c.conn.SetReadDeadline(d)
	}
	var deadline time.Time
	if value, ok := ctx.Deadline(); ok {
		deadline = value
	} else if write {
		// A context without a deadline must not hang forever on a dead peer.
		deadline = time.Now().Add(30 * time.Second)
	}
	if err := createDeadline(deadline); err != nil {
		return nil, err
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	if ctx.Done() == nil {
		close(done)
	} else {
		go func() {
			defer close(done)
			select {
			case <-ctx.Done():
				_ = createDeadline(time.Now())
			case <-stop:
			}
		}()
	}
	return func() {
		close(stop)
		<-done
		_ = createDeadline(time.Time{})
	}, nil
}

// Close sends a best-effort close frame and then unblocks readers/writers.
func (c *WebSocketConn) Close() error {
	if c == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = c.writeControl(ctx, 0x8, []byte{0x03, 0xe8})
	cancel()
	c.closeSocket()
	return nil
}

func (c *WebSocketConn) closeSocket() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.conn.Close()
	})
}

var _ FrameSocket = (*WebSocketConn)(nil)

// ProblemWriter is shared by the transport HTTP handlers.
func writeTransportProblem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"type":"about:blank","title":%q,"status":%d,"code":%q}`+"\n", http.StatusText(status), status, code)
}
