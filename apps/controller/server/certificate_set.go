package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

// A set references only already-approved actual installations. Creating this
// immutable metadata performs no Agent execution or provider operation.
func (s *Server) registerCertificateSetRoutes() {
	s.mux.HandleFunc("POST /api/v1/certificates/node-sets", func(w http.ResponseWriter, r *http.Request) {
		identity, requestID, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), identity.SubjectID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		if r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Content-Encoding") != "" {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if err != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		if gatewayDeploymentUniqueJSON(decoder) != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		var body struct {
			InstallTaskIDs []string `json:"installTaskIds"`
		}
		decoder = json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&body) != nil || len(body.InstallTaskIDs) < 2 || len(body.InstallTaskIDs) > 8 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		entries := []transport.CertificateSetEntry{}
		for _, id := range body.InstallTaskIDs {
			if !deploymentID.MatchString(id) || (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(r.Context(), identity.SubjectID, id) != nil {
				releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
				return
			}
			task, err := s.Tasks.Get(r.Context(), id)
			if err != nil || task.ActionID != transport.CertificateInstallAction || task.Status != tasks.StatusCompleted || len(task.Targets) != 1 || task.Targets[0].State != tasks.TargetSucceeded {
				releaseSourceJSON(w, 409, map[string]string{"code": "INSTALLATION_NOT_COMPLETE"})
				return
			}
			proof, err := transport.ReadCertificateChain(r.Context(), s.DB, id)
			if err != nil || proof.PlanSHA256 != task.PlanHash || proof.NodeID != task.Targets[0].NodeID || !time.Now().Before(proof.Result.NotAfter) {
				releaseSourceJSON(w, 409, map[string]string{"code": "INSTALLATION_PROOF_NOT_CURRENT"})
				return
			}
			if s.Identity.Authorize(r.Context(), auth.Check{SubjectID: identity.SubjectID, Action: certificates.ActionCertificateRead, NodeIDs: []string{proof.NodeID}}) != nil {
				releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
				return
			}
			entries = append(entries, transport.CertificateSetEntryFromReceipt(proof))
		}
		set, err := transport.FreezeCertificateSet(entries)
		if err != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_NODE_CERTIFICATE_SET"})
			return
		}
		set.CreatedAt = time.Now().UTC()
		err = s.DB.InTx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
			if err := transport.RecordCertificateSetTx(ctx, tx, set); err != nil {
				return err
			}
			ids := make([]string, 0, len(set.Entries))
			for _, entry := range set.Entries {
				ids = append(ids, entry.NodeID)
			}
			return s.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: identity.SubjectID, Action: "certificates.node_sets.create", Result: "success", RequestID: requestID, NodeIDs: ids, Source: audit.SourceWeb, ReasonCode: "node_certificate_set_frozen", At: set.CreatedAt})
		})
		if err != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "CERTIFICATE_SET_NOT_COMMITTED"})
			return
		}
		releaseSourceJSON(w, 201, map[string]any{"certificateId": set.ID, "entries": set.Entries})
	})
}
