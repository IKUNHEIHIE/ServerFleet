package transport

import "context"

// Close fences new activations, closes only sockets owned by this hub and waits
// for their domain callbacks before the composition closes its database.
func (h *ControllerLinks) Close(ctx context.Context) error {
	if h == nil || ctx == nil {
		return ErrLinkRejected
	}
	h.mu.Lock()
	h.closed = true
	links := make([]*controllerLink, 0, len(h.all))
	for l := range h.all {
		links = append(links, l)
	}
	h.mu.Unlock()
	for _, l := range links {
		_ = l.socket.Close()
	}
	for _, l := range links {
		select {
		case <-l.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
