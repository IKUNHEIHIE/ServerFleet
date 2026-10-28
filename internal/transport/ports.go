// Package transport owns WSS/mTLS framing, not user authorization or task policy.
package transport

import (
	"context"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

// Sender is a provisional seam for the Controller task service. It has no
// implementation and is never exposed directly to Web, AI, or plugins.
type Sender interface {
	Send(context.Context, string, *agentv1.Envelope) error
}
