package server

import (
	"net/http"

	"serverfleet.local/serverfleet/internal/publishing"
)

func (s *Server) registerPublicationDNSRoutes() {
	s.mux.HandleFunc("GET /api/v1/publications/{publicationId}/dns-tasks", func(w http.ResponseWriter, r *http.Request) {
		actor, _, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), actor.SubjectID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		plan, _, err := s.Publishing.Get(r.Context(), actor.SubjectID, r.PathValue("publicationId"))
		if err != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "PUBLICATION_READ_DENIED"})
			return
		}
		batch, err := publishing.FindDNSBatch(r.Context(), s.DB, plan.ID)
		if err != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "PUBLICATION_DNS_NOT_OBSERVED"})
			return
		}
		if batch.Descriptor.PlanSHA256 != publishing.PublicationPlanSHA256(plan) {
			releaseSourceJSON(w, 409, map[string]string{"code": "PUBLICATION_DNS_BINDING_CHANGED"})
			return
		}
		gate := &composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}
		for _, id := range []string{batch.ApplyTaskID, batch.RollbackTaskID} {
			if id != "" && gate.AuthorizeTaskRead(r.Context(), actor.SubjectID, id) != nil {
				releaseSourceJSON(w, 403, map[string]string{"code": "TASK_READ_DENIED"})
				return
			}
		}
		releaseSourceJSON(w, 200, batch)
	})
}
