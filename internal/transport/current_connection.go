package transport

import "context"

// CurrentConnection observes a CURRENT verified link without granting any task
// capability. Unlike FileConnection it does not require the file profile.
func (h *ControllerLinks) CurrentConnection(ctx context.Context, node string) (LinkObservation, error) {
	if h == nil || ctx == nil || ctx.Err() != nil {
		return LinkObservation{}, ErrLinkRejected
	}
	if h.cfg.RuntimeGate != nil && h.cfg.RuntimeGate(ctx) != nil {
		return LinkObservation{}, ErrLinkRejected
	}
	h.mu.Lock()
	l := h.links[node]
	ok := l != nil && l.active
	h.mu.Unlock()
	if !ok {
		return LinkObservation{}, ErrLinkRejected
	}
	id, e := h.verifier.Verify(ctx, l.tls, node)
	if e != nil || id != l.observed.Identity || !h.current(l) {
		return LinkObservation{}, ErrLinkRejected
	}
	return l.observed, nil
}

// HasCurrentCapabilities checks only the authenticated link's negotiated
// capabilities. It does not grant task or node access to the caller.
func (h *ControllerLinks) HasCurrentCapabilities(ctx context.Context, node string, actions ...string) bool {
	observation, err := h.CurrentConnection(ctx, node)
	if err != nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	link := h.links[node]
	if link == nil || !link.active || link.observed != observation {
		return false
	}
	for _, action := range actions {
		if !link.granted[action] {
			return false
		}
	}
	return true
}
