package transport

import (
	"errors"
	"testing"
)

func TestRejectionCodeMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"protocol_mismatch", invalid(InvalidProtocol, "envelope.protocol_version"), "INVALID_PROTOCOL"},
		{"limit_exceeded", invalid(LimitExceeded, "envelope.encoded_size"), "LIMIT_EXCEEDED"},
		{"connection_node_mismatch", connectionError(ConnectionNodeMismatch), "CONNECTION_NODE_MISMATCH"},
		{"frame_malformed", frameError(FrameMalformed), "FRAME_MALFORMED"},
		{"wrapped_validation", errors.Join(errors.New("outer"), invalid(InvalidProtocol, "x")), "INVALID_PROTOCOL"},
		{"generic", ErrLinkRejected, "LINK_REJECTED"},
		{"nil", nil, "LINK_REJECTED"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rejectionCode(c.err); got != c.want {
				t.Fatalf("rejectionCode(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}
