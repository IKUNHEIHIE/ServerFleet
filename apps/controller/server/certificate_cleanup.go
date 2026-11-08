package server

import (
	"context"
	"time"
)

// Retry only journal-owned, known pending outcomes, on behalf of the original
// owner under their CURRENT authorization. Lease-expired/unknown outcomes are
// never replayed. Quarantine blocks the loop just as it blocks management HTTP.
func (s *Server) runCertificateCleanup(ctx context.Context) {
	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.CertificateCleanup == nil || s.runtimeGate == nil || s.runtimeGate(ctx) != nil {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
		_, _ = s.CertificateCleanup.RetryDue(attempt, 10)
		cancel()
	}
}
