package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/tasks"
)

const actionDNSRecordApply = "dns.record.apply"

type dnsTaskRecord struct {
	Type     dns.RecordType `json:"type"`
	Name     string         `json:"name"`
	Content  string         `json:"content"`
	TTL      int            `json:"ttl"`
	Proxied  bool           `json:"proxied"`
	Priority *int           `json:"priority,omitempty"`
}

func (r dnsTaskRecord) input() dns.RecordInput {
	return dns.RecordInput{Type: r.Type, Name: r.Name, Content: r.Content, TTL: r.TTL, Proxied: r.Proxied, Priority: r.Priority}
}
func parseDNSTaskRecord(raw []byte) (dnsTaskRecord, error) {
	var record dnsTaskRecord
	if len(raw) == 0 || len(raw) > 10<<10 {
		return record, tasks.ErrInvalidTask
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if gatewayDeploymentUniqueJSON(decoder) != nil {
		return record, tasks.ErrInvalidTask
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return record, tasks.ErrInvalidTask
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || dns.ValidateExecutionRecord(record.input()) != nil {
		return record, tasks.ErrInvalidTask
	}
	return record, nil
}
func dnsRecordSnapshot(record dns.Record) string {
	record.ModifiedAt = record.ModifiedAt.UTC()
	raw, _ := json.Marshal(record)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (m *publicCertificateExecution) mutationParams(t tasks.Task) (map[string]string, dnsTaskRecord, error) {
	var p map[string]string
	var record dnsTaskRecord
	if t.ActionID != actionDNSRecordApply || t.TargetType != tasks.TargetTypeController || t.TargetResourceID != m.authorityID || len(t.Targets) != 1 || t.Targets[0].NodeID != m.authorityID || t.Source != "web" || t.ScopeRevision != 0 || t.PluginID != "" || t.OperationClass != tasks.OperationClassModifying || !t.ApprovalRequired || json.Unmarshal([]byte(t.Params), &p) != nil || !deploymentID.MatchString(p["zoneId"]) || p["profileSha256"] != m.profileSHA {
		return nil, record, tasks.ErrInvalidTask
	}
	switch p["operation"] {
	case "create":
		if p["recordId"] != "" || p["beforeSha256"] != "" {
			return nil, record, tasks.ErrInvalidTask
		}
	case "update", "delete":
		if !deploymentID.MatchString(p["recordId"]) || !isLowerSHA256(p["beforeSha256"]) {
			return nil, record, tasks.ErrInvalidTask
		}
	default:
		return nil, record, tasks.ErrInvalidTask
	}
	if p["operation"] == "delete" {
		if len(p) != 5 {
			return nil, record, tasks.ErrInvalidTask
		}
		return p, record, nil
	}
	count, err := strconv.Atoi(p["recordParts"])
	if err != nil || count < 1 || count > 14 || len(p) != 8+count {
		return nil, record, tasks.ErrInvalidTask
	}
	var encoded strings.Builder
	for i := 0; i < count; i++ {
		part := p["recordPart"+strconv.Itoa(i)]
		if part == "" || len(part) > 1024 {
			return nil, record, tasks.ErrInvalidTask
		}
		encoded.WriteString(part)
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded.String())
	if err != nil {
		return nil, record, tasks.ErrInvalidTask
	}
	record, err = parseDNSTaskRecord(raw)
	if err != nil || string(record.Type) != p["recordType"] || record.Name != p["recordName"] {
		return nil, record, tasks.ErrInvalidTask
	}
	return p, record, nil
}
func isLowerSHA256(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}
func (m *publicCertificateExecution) mutationCurrent(ctx context.Context, t tasks.Task, p map[string]string) error {
	binding, err := m.s.DNS.ExecutionBinding(ctx, t.SubmitterID, p["zoneId"])
	if err != nil || binding.CredentialID != m.o.DNSCredentialID {
		return auth.ErrDenied
	}
	if p["operation"] == "create" {
		return nil
	}
	records, err := m.s.DNS.ListRecords(ctx, t.SubmitterID, p["zoneId"])
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.ID == p["recordId"] && record.ZoneID == p["zoneId"] && dnsRecordSnapshot(record) == p["beforeSha256"] {
			return nil
		}
	}
	return auth.ErrDenied
}

func dnsMutationFailure(err error) tasks.NodeResultCode {
	// Only an explicit DNS-owner rejection proves no provider effect. A lost
	// local commit, callback/post-authorization failure or transport is UNKNOWN.
	if errors.Is(err, dns.ErrProviderFailed) {
		return tasks.NodeResultFailed
	}
	return tasks.NodeResultUnknown
}

type dnsMutationWorker struct{ manager *publicCertificateExecution }

func (w dnsMutationWorker) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	m := w.manager
	result := func(code tasks.NodeResultCode, message string) (map[string]tasks.WorkerResult, error) {
		return map[string]tasks.WorkerResult{m.authorityID: {Code: code, Message: message}}, nil
	}
	p, record, err := m.mutationParams(t)
	if err != nil {
		return result(tasks.NodeResultFailed, "DNS mutation plan rejected")
	}
	lockKey := p["zoneId"] + "/" + p["recordId"]
	if p["operation"] == "create" {
		lockKey = p["zoneId"] + "/" + string(record.Type) + "/" + record.Name
	}
	lockDigest := sha256.Sum256([]byte(lockKey))
	lock := &m.dnsRecordLocks[int(lockDigest[0])%len(m.dnsRecordLocks)]
	lock.Lock()
	defer lock.Unlock()
	scope := m.taskScope(t, p["zoneId"])
	ctx, cancel := context.WithTimeout(m.scope(ctx, scope), time.Minute)
	defer cancel()
	if m.current(ctx, scope) != nil {
		return result(tasks.NodeResultFailed, "DNS mutation authorization or frozen record changed")
	}
	switch p["operation"] {
	case "create":
		created, e := m.s.DNS.CreateRecord(ctx, t.SubmitterID, t.RequestID, p["zoneId"], record.input())
		if e != nil {
			return result(dnsMutationFailure(e), "DNS create result not durably observed; reconcile provider and local records before any new task")
		}
		return result(tasks.NodeResultSucceeded, "Created DNS record "+created.ID+" in zone "+created.ZoneID)
	case "update":
		updated, e := m.s.DNS.UpdateRecord(ctx, t.SubmitterID, t.RequestID, p["recordId"], record.input())
		if e != nil {
			return result(dnsMutationFailure(e), "DNS update result not durably observed; no automatic replay")
		}
		return result(tasks.NodeResultSucceeded, "Updated DNS record "+updated.ID+" in zone "+updated.ZoneID)
	case "delete":
		if e := m.s.DNS.DeleteRecord(ctx, t.SubmitterID, t.RequestID, p["recordId"]); e != nil {
			return result(dnsMutationFailure(e), "DNS delete result not durably observed; no automatic replay")
		}
		return result(tasks.NodeResultSucceeded, "Deleted exact approved DNS record "+p["recordId"]+" from zone "+p["zoneId"])
	}
	return result(tasks.NodeResultFailed, "DNS mutation operation rejected")
}
func (s *Server) registerDNSRecordTaskRoutes() {
	s.mux.HandleFunc("POST /api/v1/dns/record-tasks", func(w http.ResponseWriter, r *http.Request) {
		id, requestID, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.Identity.AuthorizeAdministrator(r.Context(), id.SubjectID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "NOT_AUTHORIZED"})
			return
		}
		m := s.PublicCertificates
		if m == nil {
			releaseSourceJSON(w, 503, map[string]string{"code": "DNS_DEPLOYMENT_UNAVAILABLE"})
			return
		}
		body, ok := publicCertificateBody(w, r, 24<<10)
		if !ok {
			return
		}
		if !deploymentID.MatchString(body["zoneId"]) || !deploymentID.MatchString(body["idempotencyKey"]) {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		p := map[string]string{"operation": body["operation"], "zoneId": body["zoneId"], "profileSha256": m.profileSHA, "recordId": "", "beforeSha256": ""}
		if body["operation"] == "update" || body["operation"] == "delete" {
			if !deploymentID.MatchString(body["recordId"]) {
				releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
				return
			}
			records, e := s.DNS.ListRecords(r.Context(), id.SubjectID, body["zoneId"])
			if e != nil {
				releaseSourceJSON(w, 403, map[string]string{"code": "DNS_RECORD_READ_DENIED"})
				return
			}
			found := false
			for _, record := range records {
				if record.ID == body["recordId"] && record.ZoneID == body["zoneId"] {
					p["recordId"] = record.ID
					p["beforeSha256"] = dnsRecordSnapshot(record)
					found = true
					break
				}
			}
			if !found {
				releaseSourceJSON(w, 409, map[string]string{"code": "DNS_RECORD_NOT_CURRENT"})
				return
			}
		}
		expected := 3
		if body["operation"] != "create" {
			expected++
		}
		if body["operation"] != "delete" {
			expected++
			record, e := parseDNSTaskRecord([]byte(body["recordJson"]))
			if e != nil {
				releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_RECORD"})
				return
			}
			canonical, _ := json.Marshal(record)
			encoded := base64.StdEncoding.EncodeToString(canonical)
			parts := 0
			for len(encoded) > 0 {
				size := min(len(encoded), 1024)
				p["recordPart"+strconv.Itoa(parts)] = encoded[:size]
				encoded = encoded[size:]
				parts++
			}
			p["recordParts"] = strconv.Itoa(parts)
			p["recordType"] = string(record.Type)
			p["recordName"] = record.Name
		}
		if len(body) != expected {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		canonical, _ := json.Marshal(p)
		candidate := tasks.Task{ActionID: actionDNSRecordApply, TargetType: tasks.TargetTypeController, TargetResourceID: m.authorityID, Targets: []tasks.Target{{NodeID: m.authorityID}}, Source: "web", OperationClass: tasks.OperationClassModifying, ApprovalRequired: true, Params: string(canonical), SubmitterID: id.SubjectID}
		parsed, _, err := m.mutationParams(candidate)
		if err != nil {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		if m.mutationCurrent(r.Context(), candidate, parsed) != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "DNS_ZONE_OR_RECORD_NOT_CURRENT"})
			return
		}
		meta, err := s.Credentials.ServiceMetadata(r.Context(), id.SubjectID, m.o.DNSCredentialID)
		if err != nil || meta.Kind != "cloudflare_token" || meta.Revision != m.o.DNSCredentialRevision {
			releaseSourceJSON(w, 403, map[string]string{"code": "DNS_CREDENTIAL_NOT_OWNED_OR_CURRENT"})
			return
		}
		task, err := s.Tasks.SubmitController(r.Context(), tasks.SubmitRequest{SubjectID: id.SubjectID, RequestID: requestID, Name: "Apply approved DNS record change", ActionID: actionDNSRecordApply, Source: audit.SourceWeb, OperationClass: tasks.OperationClassModifying, RequireApproval: true, Params: p, IdempotencyKey: "dns-record-" + backupTaskKey("apply", body["idempotencyKey"])})
		if err != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "DNS_RECORD_TASK_REJECTED"})
			return
		}
		releaseSourceJSON(w, 202, map[string]any{"taskId": task.ID, "status": task.Status, "planHash": task.PlanHash, "approvalRequired": task.ApprovalRequired})
	})
}
