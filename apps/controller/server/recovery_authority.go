package server

import (
	"net/http"

	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
)

// RuntimeHTTP verifies the live fence both before dispatch and before writing
// this response. This is an observation only, never a release/fence endpoint.
func (s *Server) registerRecoveryAuthorityRoutes() {
	s.mux.HandleFunc("GET /api/v1/system/recovery-authority", func(w http.ResponseWriter, r *http.Request) {
		identity, _, e := s.authenticateIdentity(r)
		if r.TLS == nil || e != nil || s.Identity.AuthorizeAdministrator(r.Context(), identity.SubjectID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "ADMINISTRATOR_REQUIRED"})
			return
		}
		binding, e := recovery.ReadResidentAuthority(r.Context(), s.DB)
		if e != nil {
			releaseSourceJSON(w, 503, map[string]string{"code": "AUTHORITY_UNKNOWN"})
			return
		}
		if binding == nil {
			releaseSourceJSON(w, 200, map[string]any{"mode": "standard", "continuousRecoveryFence": false, "recoveryActivationObserved": false})
			return
		}
		receipt, e := recovery.ReadReconciliation(r.Context(), s.DB)
		if e != nil {
			if !storage.IsNotFound(e) {
				releaseSourceJSON(w, 503, map[string]string{"code": "RECONCILIATION_UNKNOWN"})
				return
			}
			releaseSourceJSON(w, 200, map[string]any{"mode": "supervised", "authorityId": binding.AuthorityID, "continuousRecoveryFence": false, "recoveryActivationObserved": false})
			return
		}
		// A restored DB cannot be started through the standard profile; the persisted
		// authority binding and outer runtime gate still apply if flags are omitted.
		releaseSourceJSON(w, 200, map[string]any{"mode": "restored_supervised", "authorityId": binding.AuthorityID, "trustGeneration": receipt.TrustGeneration, "revocationSequence": receipt.RevocationSequence, "holdId": receipt.HoldID, "continuousRecoveryFence": true, "recoveryActivationObserved": true, "agentReenrollmentRequired": true})
	})
}
