package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"

	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/tasks"
)

func publicCertificateBody(w http.ResponseWriter, r *http.Request, max int64) (map[string]string, bool) {
	if r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Content-Encoding") != "" {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return nil, false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if gatewayDeploymentUniqueJSON(decoder) != nil {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return nil, false
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return nil, false
	}
	var fields map[string]string
	if json.Unmarshal(raw, &fields) != nil {
		releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
		return nil, false
	}
	return fields, true
}

type publicCertificateChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) publicCertificateZones(ctx context.Context, subject string) ([]publicCertificateChoice, error) {
	eligibleZones := []publicCertificateChoice{}
	accounts, err := s.DNS.ListAccounts(ctx, subject)
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		credential, err := s.DNS.AccountExecutionBinding(ctx, subject, account.ID)
		if err != nil || credential != s.PublicCertificates.o.DNSCredentialID {
			continue
		}
		zones, err := s.DNS.ListZones(ctx, subject, account.ID)
		if err != nil {
			return nil, err
		}
		for _, zone := range zones {
			if zone.Status == dns.ZoneActive &&
				s.PublicCertificates.zone(ctx, tasks.Task{SubmitterID: subject}, zone.ID, zone.Name) == nil {
				eligibleZones = append(eligibleZones, publicCertificateChoice{ID: zone.ID, Name: zone.Name})
			}
		}
	}
	return eligibleZones, nil
}

func (s *Server) registerPublicCertificateRoutes() {
	s.registerDNSProviderTaskRoutes()
	s.mux.HandleFunc("GET /api/v1/certificates/public-tasks/{taskId}", func(w http.ResponseWriter, r *http.Request) {
		id, _, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), id.SubjectID) != nil || (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(r.Context(), id.SubjectID, r.PathValue("taskId")) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		task, err := s.Tasks.Get(r.Context(), r.PathValue("taskId"))
		if err != nil || task.ActionID != actionPublicCertificateIssue || len(task.Targets) != 1 {
			releaseSourceJSON(w, 404, map[string]string{"code": "CERTIFICATE_ORDER_NOT_FOUND"})
			return
		}
		order, err := certificates.ReadPublicOrder(r.Context(), s.DB, task.ID)
		if err != nil {
			releaseSourceJSON(w, 409, map[string]any{"code": "CERTIFICATE_ORDER_NOT_OBSERVED", "status": task.Status})
			return
		}
		if order.PlanSHA256 != task.PlanHash || order.NodeID != task.Targets[0].NodeID {
			releaseSourceJSON(w, 409, map[string]string{"code": "CERTIFICATE_ORDER_BINDING_CHANGED"})
			return
		}
		releaseSourceJSON(w, 200, map[string]any{"taskId": task.ID, "taskStatus": task.Status, "orderStatus": order.Status, "csrTaskId": order.CSRTaskID, "nodeId": order.NodeID, "domain": order.Domain, "keyRef": order.KeyRef, "chainSha256": order.ChainSHA256, "certificatePemBase64": base64.StdEncoding.EncodeToString(order.ChainPEM), "notAfter": order.NotAfter, "pendingCleanupCount": len(order.PendingCleanup), "errorCode": order.ErrorCode, "observedAt": order.UpdatedAt, "installed": false})
	})
}
