package transport

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

// ConnectionBinding must come from a trusted caller, never from an inbound
// Envelope. These strings and this type do NOT prove TLS identity or authority.
type ConnectionBinding struct {
	NodeID       string
	ConnectionID string
	Direction    Direction
}

type ConnectionErrorCode string

const (
	ConnectionInvalidBinding    ConnectionErrorCode = "CONNECTION_INVALID_BINDING"
	ConnectionUnbound           ConnectionErrorCode = "CONNECTION_UNBOUND"
	ConnectionClosed            ConnectionErrorCode = "CONNECTION_CLOSED"
	ConnectionDirectionMismatch ConnectionErrorCode = "CONNECTION_DIRECTION_MISMATCH"
	ConnectionNodeMismatch      ConnectionErrorCode = "CONNECTION_NODE_MISMATCH"
	ConnectionIDMismatch        ConnectionErrorCode = "CONNECTION_ID_MISMATCH"
	ConnectionSequenceRejected  ConnectionErrorCode = "CONNECTION_SEQUENCE_REJECTED"
)

// ConnectionError reports a fixed category without frame data or binding IDs.
// Earlier decode/semantic failures retain *FrameError / *ValidationError.
type ConnectionError struct {
	Code ConnectionErrorCode
}

func (e *ConnectionError) Error() string { return "connection guard: " + string(e.Code) }

func connectionError(code ConnectionErrorCode) error { return &ConnectionError{Code: code} }

// ConnectionGuard is an offline, single-connection, single-inbound-direction
// guard. Its nil/zero value cannot receive. Copies of a constructed handle share
// state (including terminal close); they do not copy a mutex or reset a stream.
type ConnectionGuard struct {
	state *connectionState
}

type connectionState struct {
	mu        sync.Mutex
	binding   ConnectionBinding
	highWater uint64
	closed    bool
}

func validBindingID(id string) bool {
	return id != "" && utf8.ValidString(id) && !strings.ContainsFunc(id, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
}

// NewConnectionGuard snapshots the explicit binding. New instances start at
// sequence high-water zero; there is no in-place rebind, reset or resume API.
func NewConnectionGuard(binding ConnectionBinding) (*ConnectionGuard, error) {
	if !validBindingID(binding.NodeID) || !validBindingID(binding.ConnectionID) ||
		(binding.Direction != AgentToController && binding.Direction != ControllerToAgent) {
		return nil, connectionError(ConnectionInvalidBinding)
	}
	return &ConnectionGuard{state: &connectionState{binding: binding}}, nil
}

// Receive reuses bounded raw decoding and semantic validation before reconciling
// the binding and committing a strictly higher inbound sequence. The entire
// bounded operation and Close share one lock, so rejected frames never consume
// a sequence and concurrent comparisons/commits are atomic. Input and supplied
// policies must not be modified concurrently by the caller.
func (g *ConnectionGuard) Receive(raw []byte, ctx ValidationContext, semantic ValidationLimits, limits FrameDecodeLimits) (*agentv1.Envelope, error) {
	if g == nil || g.state == nil {
		return nil, connectionError(ConnectionUnbound)
	}
	s := g.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, connectionError(ConnectionClosed)
	}
	decoded, err := DecodeEnvelopeFrame(raw, ctx, semantic, limits)
	if err != nil {
		return nil, err
	}
	if ctx.Direction != s.binding.Direction {
		return nil, connectionError(ConnectionDirectionMismatch)
	}
	if decoded.NodeId != s.binding.NodeID {
		return nil, connectionError(ConnectionNodeMismatch)
	}
	if decoded.ConnectionId != s.binding.ConnectionID {
		return nil, connectionError(ConnectionIDMismatch)
	}
	if decoded.Sequence <= s.highWater {
		return nil, connectionError(ConnectionSequenceRejected)
	}
	s.highWater = decoded.Sequence
	return decoded, nil
}

// Close terminally invalidates this instance. It is idempotent and nil-safe.
// An in-progress Receive may commit before Close obtains the lock and deliver
// its result later. Any Receive invoked after Close returns fails on all copies.
func (g *ConnectionGuard) Close() {
	if g == nil || g.state == nil {
		return
	}
	s := g.state
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

// HighWatermark returns a synchronized diagnostic snapshot, including after
// Close. Nil/unbound guards report zero; this accessor does not prove binding.
func (g *ConnectionGuard) HighWatermark() uint64 {
	if g == nil || g.state == nil {
		return 0
	}
	s := g.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.highWater
}
