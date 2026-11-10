package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

const actionPublicationDNSApply = "publishing.dns.apply"
const actionPublicationDNSRollback = "publishing.dns.rollback"

type publicationDNSStage struct{ manager *publicCertificateExecution }

func (publicationDNSStage) CanResumeDNSStage() {}
func (m *publicCertificateExecution) batchParams(batch publishing.DNSBatch, rollback bool) map[string]string {
	p := map[string]string{"batchId": batch.ID, "publicationId": batch.Descriptor.PlanID, "targetRevision": strconv.FormatUint(batch.Descriptor.TargetRevision, 10), "zoneId": batch.Descriptor.ZoneID, "profileSha256": batch.Descriptor.ProfileSHA256}
	for i, record := range batch.Descriptor.Records {
		raw, _ := json.Marshal(record)
		p["record"+strconv.Itoa(i)] = string(raw)
	}
	return p
}
func (m *publicCertificateExecution) parseBatchTask(ctx context.Context, t tasks.Task) (publishing.DNSBatch, bool, error) {
	rollback := t.ActionID == actionPublicationDNSRollback
	if t.ActionID != actionPublicationDNSApply && !rollback || t.TargetType != tasks.TargetTypeController || t.TargetResourceID != m.authorityID || len(t.Targets) != 1 || t.Targets[0].NodeID != m.authorityID || t.Source != "web" || t.PluginID != "" || t.ScopeRevision != 0 || t.OperationClass != tasks.OperationClassModifying || !t.ApprovalRequired {
		return publishing.DNSBatch{}, false, tasks.ErrInvalidTask
	}
	var p map[string]string
	if json.Unmarshal([]byte(t.Params), &p) != nil || !isLowerSHA256(p["batchId"]) {
		return publishing.DNSBatch{}, false, tasks.ErrInvalidTask
	}
	batch, err := publishing.ReadDNSBatch(ctx, m.s.DB, p["batchId"])
	if err != nil || batch.Descriptor.ProfileSHA256 != m.profileSHA {
		return batch, rollback, tasks.ErrInvalidTask
	}
	canonical, _ := json.Marshal(m.batchParams(batch, rollback))
	actual, _ := json.Marshal(p)
	if string(actual) != string(canonical) {
		return batch, rollback, tasks.ErrInvalidTask
	}
	taskID, plan := batch.ApplyTaskID, batch.ApplyPlanSHA256
	if rollback {
		taskID, plan = batch.RollbackTaskID, batch.RollbackPlanSHA256
	}
	if taskID != t.ID || plan != t.PlanHash {
		return batch, rollback, tasks.ErrInvalidTask
	}
	return batch, rollback, nil
}
func (m *publicCertificateExecution) batchAuthority(ctx context.Context, t tasks.Task, batch publishing.DNSBatch, rollback bool) error {
	if m.s.Publishing == nil || m.s.runtimeGate(ctx) != nil || m.s.Identity.AuthorizeAdministrator(ctx, t.SubmitterID) != nil {
		return publishing.ErrProductionUnavailable
	}
	meta, e := m.s.Credentials.ServiceMetadata(ctx, t.SubmitterID, m.o.DNSCredentialID)
	if e != nil || meta.Kind != "cloudflare_token" || meta.Revision != m.o.DNSCredentialRevision {
		return auth.ErrDenied
	}
	if err := m.s.Publishing.CheckDNSBatch(ctx, t.SubmitterID, t.RequestID, batch, rollback); err != nil {
		return err
	}
	binding, err := m.s.DNS.ExecutionBinding(ctx, t.SubmitterID, batch.Descriptor.ZoneID)
	if err != nil || binding.CredentialID != m.o.DNSCredentialID {
		return auth.ErrDenied
	}
	// Domain and exact selected gateway target addresses still belong to this
	// frozen publication. Rollback does not require an old node to be online.
	plan, _, err := m.s.PublishingRepo.Get(ctx, batch.Descriptor.PlanID)
	if err != nil {
		return err
	}
	name := strings.TrimPrefix(plan.Domain, "*.")
	if name != binding.Name && !strings.HasSuffix(name, "."+binding.Name) {
		return publishing.ErrProductionTarget
	}
	if !rollback {
		addresses, e := m.batchAddresses(ctx, t.SubmitterID, plan)
		if e != nil || len(addresses) != len(batch.Descriptor.Records) {
			return publishing.ErrProductionTarget
		}
		for i, address := range addresses {
			if batch.Descriptor.Records[i].Content != address || batch.Descriptor.Records[i].Name != plan.Domain {
				return publishing.ErrProductionTarget
			}
		}
	}
	return nil
}
func (m *publicCertificateExecution) batchCurrent(ctx context.Context, scope *publicCertificateScope) error {
	batch, rollback, err := m.parseBatchTask(ctx, scope.task)
	if err != nil || scope.zoneID != batch.Descriptor.ZoneID || scope.dnsBatchIndex < 0 || scope.dnsBatchIndex >= len(batch.Observations) {
		return auth.ErrDenied
	}
	if rollback && batch.State != "rolling_back" || !rollback && batch.State != "running" {
		return auth.ErrDenied
	}
	state := batch.Observations[scope.dnsBatchIndex].State
	if rollback && state != "deleting" || !rollback && state != "creating" && !(state == "pending" && batch.Descriptor.Records[scope.dnsBatchIndex].ExistingID != "") {
		return auth.ErrDenied
	}
	return m.batchAuthority(ctx, scope.task, batch, rollback)
}
func (m *publicCertificateExecution) checkBatchOperation(ctx context.Context, scope *publicCertificateScope, operation string) error {
	if scope.task.ActionID == actionPublicationDNSApply && operation != "create record" && operation != "delete record" && operation != "read record" {
		return auth.ErrDenied
	}
	if scope.task.ActionID == actionPublicationDNSRollback && operation != "delete record" && operation != "read record" {
		return auth.ErrDenied
	}
	// The create owner's compensation may delete only its own just-returned ID;
	// it remains under the same running task and current batch index.
	return m.batchCurrent(ctx, scope)
}
func (m *publicCertificateExecution) batchAddresses(ctx context.Context, subject string, plan publishing.Plan) ([]string, error) {
	raw, err := (publicationGatewayAddresses{gateways: m.s.Gateway, nodes: m.s.NodesRepo}).Addresses(ctx, subject, plan.GatewayIDs)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	for _, address := range raw {
		ip := net.ParseIP(address)
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			return nil, publishing.ErrProductionTarget
		}
		text := ip.String()
		if !seen[text] {
			seen[text] = true
			out = append(out, text)
		}
	}
	sort.Strings(out)
	if len(out) < 1 || len(out) > 8 {
		return nil, publishing.ErrProductionTarget
	}
	return out, nil
}
func (m *publicCertificateExecution) freezeBatch(ctx context.Context, subject, request string, plan publishing.Plan) (publishing.DNSBatch, error) {
	existing, err := publishing.FindDNSBatch(ctx, m.s.DB, plan.ID)
	if err == nil {
		if existing.Descriptor.PlanSHA256 != publishing.PublicationPlanSHA256(plan) || existing.Descriptor.ProfileSHA256 != m.profileSHA {
			return existing, publishing.ErrProductionTarget
		}
		return existing, nil
	}
	if !storage.IsNotFound(err) {
		return publishing.DNSBatch{}, err
	}
	addresses, err := m.batchAddresses(ctx, subject, plan)
	if err != nil {
		return publishing.DNSBatch{}, err
	}
	records, err := m.s.DNS.ListRecords(ctx, subject, plan.ZoneID)
	if err != nil {
		return publishing.DNSBatch{}, err
	}
	batch := publishing.DNSBatch{Descriptor: publishing.DNSBatchDescriptor{PlanID: plan.ID, PlanSHA256: publishing.PublicationPlanSHA256(plan), TargetRevision: plan.TargetRevision, ZoneID: plan.ZoneID, ProfileSHA256: m.profileSHA}, State: "reserved", UpdatedAt: time.Now().UTC()}
	for _, address := range addresses {
		kind := "AAAA"
		if net.ParseIP(address).To4() != nil {
			kind = "A"
		}
		item := publishing.DNSBatchRecord{Type: kind, Name: plan.Domain, Content: address, TTL: 60}
		for _, record := range records {
			if string(record.Type) == kind && record.Name == plan.Domain && record.Content == address {
				if item.ExistingID != "" {
					return batch, publishing.ErrProductionTarget
				}
				item.ExistingID = record.ID
				item.ExistingSHA256 = dnsRecordSnapshot(record)
			}
		}
		batch.Descriptor.Records = append(batch.Descriptor.Records, item)
		batch.Observations = append(batch.Observations, publishing.DNSBatchObservation{State: "pending"})
	}
	batch.ID = publishing.DNSDescriptorSHA256(batch.Descriptor)
	if m.s.Identity.AuthorizeAdministrator(ctx, subject) != nil {
		return batch, auth.ErrDenied
	}
	if err = m.s.Publishing.CheckDNSBatch(ctx, subject, request, batch, false); err != nil {
		return batch, err
	}
	err = m.s.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if e := publishing.ReserveDNSBatchTx(ctx, tx, batch); e != nil {
			return e
		}
		return m.s.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: subject, Action: actionPublicationDNSApply, Result: "allowed", RequestID: request, Source: audit.SourceWeb, ReasonCode: "publication_dns_targets_frozen", At: batch.UpdatedAt})
	})
	return batch, err
}
func cloneDNSBatch(before publishing.DNSBatch) publishing.DNSBatch {
	after := before
	after.Observations = append([]publishing.DNSBatchObservation{}, before.Observations...)
	return after
}
func (m *publicCertificateExecution) saveBatch(ctx context.Context, t tasks.Task, before, after publishing.DNSBatch, reason string) error {
	after.UpdatedAt = time.Now().UTC()
	return m.s.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := publishing.UpdateDNSBatchTx(ctx, tx, before, after); err != nil {
			return err
		}
		return m.s.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: t.SubmitterID, Action: t.ActionID, Result: "allowed", RequestID: t.RequestID, TaskID: t.ID, Source: audit.SourceWeb, ReasonCode: reason, At: after.UpdatedAt})
	})
}
func (m *publicCertificateExecution) bindBatchTask(ctx context.Context, subject, request string, batch publishing.DNSBatch, rollback bool) (publishing.DNSBatch, error) {
	action := actionPublicationDNSApply
	current := batch.ApplyTaskID
	if rollback {
		action = actionPublicationDNSRollback
		current = batch.RollbackTaskID
	}
	if m.s.Identity.AuthorizeAdministrator(ctx, subject) != nil {
		return batch, auth.ErrDenied
	}
	if err := m.s.Publishing.CheckDNSBatch(ctx, subject, request, batch, rollback); err != nil {
		return batch, err
	}
	if current != "" {
		return batch, nil
	}
	if rollback {
		original, err := m.s.Tasks.Get(ctx, batch.ApplyTaskID)
		if err != nil || !original.Status.Terminal() {
			return batch, publishing.ErrStageAwaitingTask
		}
	}
	if m.s.Identity.AuthorizeAdministrator(ctx, subject) != nil {
		return batch, auth.ErrDenied
	}
	if err := m.s.Publishing.CheckDNSBatch(ctx, subject, request, batch, rollback); err != nil {
		return batch, err
	}
	task, err := m.s.Tasks.SubmitController(ctx, tasks.SubmitRequest{SubjectID: subject, RequestID: request, Name: "Apply or compensate frozen publication DNS records", ActionID: action, Source: audit.SourceWeb, OperationClass: tasks.OperationClassModifying, RequireApproval: true, Params: m.batchParams(batch, rollback), IdempotencyKey: "pub-dns-" + backupTaskKey(action, batch.ID)})
	if err != nil {
		return batch, err
	}
	after := cloneDNSBatch(batch)
	if rollback {
		after.RollbackTaskID = task.ID
		after.RollbackPlanSHA256 = task.PlanHash
	} else {
		after.ApplyTaskID = task.ID
		after.ApplyPlanSHA256 = task.PlanHash
	}
	if err = m.saveBatch(ctx, task, batch, after, "publication_dns_task_bound"); err != nil {
		return batch, err
	}
	return publishing.ReadDNSBatch(ctx, m.s.DB, batch.ID)
}
func (m *publicCertificateExecution) observeBatch(ctx context.Context, batch publishing.DNSBatch, rollback bool) (publishing.StageResult, error) {
	taskID := batch.ApplyTaskID
	if rollback {
		taskID = batch.RollbackTaskID
	}
	summary, _ := json.Marshal(publishing.DNSReceipt{DNSBatchID: batch.ID, TaskID: taskID})
	result := publishing.StageResult{Summary: string(summary)}
	t, err := m.s.Tasks.Get(ctx, taskID)
	if err != nil {
		return result, err
	}
	if !t.Status.Terminal() {
		return result, publishing.ErrStageAwaitingTask
	}
	if t.Status != tasks.StatusCompleted || len(t.Targets) != 1 || t.Targets[0].State != tasks.TargetSucceeded {
		return result, publishing.ErrProductionUnavailable
	}
	if rollback && batch.State != "rolled_back" || !rollback && batch.State != "applied" {
		return result, publishing.ErrProductionUnavailable
	}
	return result, nil
}
func (p publicationDNSStage) Publish(ctx context.Context, r publishing.DNSPublishRequest) (publishing.StageResult, error) {
	m := p.manager
	plan, _, err := m.s.PublishingRepo.Get(ctx, r.PlanID)
	if err != nil || plan.Domain != r.Domain || plan.TargetRevision != r.TargetRevision || plan.ZoneID != r.ZoneID {
		return publishing.StageResult{}, publishing.ErrProductionTarget
	}
	batch, err := m.freezeBatch(ctx, r.SubjectID, r.RequestID, plan)
	if err != nil {
		return publishing.StageResult{}, err
	}
	batch, err = m.bindBatchTask(ctx, r.SubjectID, r.RequestID, batch, false)
	if err != nil {
		return publishing.StageResult{}, err
	}
	return m.observeBatch(ctx, batch, false)
}
func (p publicationDNSStage) Rollback(ctx context.Context, planID string, result publishing.StageResult) error {
	m := p.manager
	var receipt publishing.DNSReceipt
	decoder := json.NewDecoder(strings.NewReader(result.Summary))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&receipt) != nil {
		return publishing.ErrProductionTarget
	}
	batch, err := publishing.ReadDNSBatch(ctx, m.s.DB, receipt.DNSBatchID)
	if err != nil || batch.Descriptor.PlanID != planID || batch.ApplyTaskID != receipt.TaskID {
		return publishing.ErrProductionTarget
	}
	subject, request, err := publishing.DNSRollbackActor(ctx)
	if err != nil {
		return err
	}
	batch, err = m.bindBatchTask(ctx, subject, request, batch, true)
	if err != nil {
		return err
	}
	_, err = m.observeBatch(ctx, batch, true)
	return err
}

// Read exact local identity immediately before a read/reuse/delete; no provider
// search by name alone and no deleting records created by a different task.
func (m *publicCertificateExecution) batchRecord(ctx context.Context, subject, zone, id, digest string) (dns.Record, error) {
	records, err := m.s.DNS.ListRecords(ctx, subject, zone)
	if err != nil {
		return dns.Record{}, err
	}
	for _, record := range records {
		if record.ID == id && record.ZoneID == zone && dnsRecordSnapshot(record) == digest {
			return record, nil
		}
	}
	return dns.Record{}, publishing.ErrProductionTarget
}

type publicationDNSWorker struct{ manager *publicCertificateExecution }

func (w publicationDNSWorker) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	m := w.manager
	result := func(code tasks.NodeResultCode, message string) (map[string]tasks.WorkerResult, error) {
		return map[string]tasks.WorkerResult{m.authorityID: {Code: code, Message: message}}, nil
	}
	// Fixed process-local stripes coordinate batch records with manual DNS tasks.
	// The durable receipt, not this lock, fences process-death replay.
	initial, _, parseErr := m.parseBatchTask(ctx, t)
	if parseErr != nil {
		return result(tasks.NodeResultFailed, "publication DNS task binding rejected")
	}
	digest := sha256.Sum256([]byte(initial.ID))
	batchLock := &m.publicationDNSLocks[int(digest[0])%len(m.publicationDNSLocks)]
	batchLock.Lock()
	defer batchLock.Unlock()
	batch, rollback, err := m.parseBatchTask(ctx, t)
	_, executionErr := m.s.Tasks.CheckExecution(ctx, t.ID, t.PlanHash)
	if err != nil || executionErr != nil || m.batchAuthority(ctx, t, batch, rollback) != nil {
		return result(tasks.NodeResultFailed, "publication DNS task or parent plan rejected")
	}
	if !rollback && batch.State != "reserved" || rollback && (batch.State == "running" || batch.State == "rolling_back" || batch.State == "rolled_back") {
		return result(tasks.NodeResultUnknown, "existing DNS effects must not be replayed")
	}
	original := batch
	after := cloneDNSBatch(batch)
	after.State = "running"
	if rollback {
		after.State = "rolling_back"
	}
	if err = m.saveBatch(ctx, t, original, after, "publication_dns_execution_reserved"); err != nil {
		return result(tasks.NodeResultUnknown, "DNS execution reservation unknown")
	}
	scope := m.taskScope(t, batch.Descriptor.ZoneID)
	ctx, cancel := context.WithTimeout(m.scope(ctx, scope), 2*time.Minute)
	defer cancel()
	for step := 0; step < len(batch.Observations); step++ {
		batch, err = publishing.ReadDNSBatch(ctx, m.s.DB, batch.ID)
		if err != nil {
			return result(tasks.NodeResultUnknown, "DNS observation unavailable")
		}
		index := step
		if rollback {
			index = len(batch.Observations) - 1 - step
		}
		scope.dnsBatchIndex = index
		if err = m.batchAuthority(ctx, t, batch, rollback); err != nil {
			return m.failBatch(ctx, t, batch, result, "publication authority changed")
		}
		record, observed := batch.Descriptor.Records[index], batch.Observations[index]
		if rollback && (observed.State == "reused" || observed.State == "pending" || observed.State == "deleted") {
			continue
		}
		if !rollback && observed.State != "pending" || rollback && observed.State != "created" {
			return m.failBatch(ctx, t, batch, result, "unresolved DNS effect; no automatic replay")
		}
		lockKey := batch.Descriptor.ZoneID + "/" + observed.RecordID
		if !rollback {
			lockKey = batch.Descriptor.ZoneID + "/" + record.Type + "/" + record.Name
		}
		lock := m.dnsRecordLock(lockKey)
		lock.Lock()
		err = m.executeBatchRecord(ctx, t, batch, index, rollback, scope)
		lock.Unlock()
		if err != nil {
			finalRead, stopRead := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			current, e := publishing.ReadDNSBatch(finalRead, m.s.DB, batch.ID)
			stopRead()
			if e == nil {
				batch = current
			}
			return m.failBatch(ctx, t, batch, result, "DNS provider or persistence outcome requires inspection")
		}
	}
	batch, err = publishing.ReadDNSBatch(ctx, m.s.DB, batch.ID)
	if err != nil {
		return result(tasks.NodeResultUnknown, "DNS terminal observation unavailable")
	}
	after = cloneDNSBatch(batch)
	after.State = "applied"
	if rollback {
		after.State = "rolled_back"
	}
	if err = m.saveBatch(ctx, t, batch, after, "publication_dns_effects_observed"); err != nil {
		return result(tasks.NodeResultUnknown, "DNS terminal commit unknown")
	}
	return result(tasks.NodeResultSucceeded, after.State+"; inspect persisted publication DNS receipt")
}
func (m *publicCertificateExecution) executeBatchRecord(ctx context.Context, t tasks.Task, batch publishing.DNSBatch, index int, rollback bool, scope *publicCertificateScope) error {
	record, observed := batch.Descriptor.Records[index], batch.Observations[index]
	before := batch
	after := cloneDNSBatch(batch)
	if !rollback && record.ExistingID != "" {
		actual, err := m.batchRecord(ctx, t.SubmitterID, batch.Descriptor.ZoneID, record.ExistingID, record.ExistingSHA256)
		if err != nil {
			return err
		}
		if err = m.s.DNS.VerifyExecutionRecord(ctx, t.SubmitterID, t.RequestID, batch.Descriptor.ZoneID, actual.ID); err != nil {
			return err
		}
		after.Observations[index] = publishing.DNSBatchObservation{State: "reused", RecordID: actual.ID, RecordSHA256: dnsRecordSnapshot(actual)}
		return m.saveBatch(ctx, t, before, after, "publication_dns_existing_record_reused")
	}
	if !rollback {
		records, err := m.s.DNS.ListRecords(ctx, t.SubmitterID, batch.Descriptor.ZoneID)
		if err != nil {
			return err
		}
		for _, current := range records {
			if string(current.Type) == record.Type && current.Name == record.Name && current.Content == record.Content {
				return publishing.ErrProductionTarget
			}
		}
	}
	if rollback {
		if _, err := m.batchRecord(ctx, t.SubmitterID, batch.Descriptor.ZoneID, observed.RecordID, observed.RecordSHA256); err != nil {
			return err
		}
		after.Observations[index].State = "deleting"
	} else {
		after.Observations[index].State = "creating"
	}
	if err := m.saveBatch(ctx, t, before, after, "publication_dns_io_intent"); err != nil {
		return err
	}
	// Read back the exact committed owner row; its timestamp is part of CAS.
	before, err := publishing.ReadDNSBatch(ctx, m.s.DB, batch.ID)
	if err != nil {
		return err
	}
	if m.current(ctx, scope) != nil {
		return auth.ErrDenied
	}
	after = cloneDNSBatch(before)
	if rollback {
		if err = m.s.DNS.DeleteRecord(ctx, t.SubmitterID, t.RequestID, observed.RecordID); err != nil {
			if errors.Is(err, dns.ErrProviderFailed) || errors.Is(err, dns.ErrProviderRecordChanged) {
				after.Observations[index].State = "created"
				if e := m.saveBatch(ctx, t, before, after, "publication_dns_delete_rejected_unapplied"); e != nil {
					return e
				}
			}
			return err
		}
		after.Observations[index].State = "deleted"
	} else {
		actual, e := m.s.DNS.CreateRecord(ctx, t.SubmitterID, t.RequestID, batch.Descriptor.ZoneID, dns.RecordInput{Type: dns.RecordType(record.Type), Name: record.Name, Content: record.Content, TTL: record.TTL})
		if e != nil {
			if errors.Is(e, dns.ErrProviderFailed) {
				after.Observations[index].State = "pending"
				if saveErr := m.saveBatch(ctx, t, before, after, "publication_dns_create_rejected_unapplied"); saveErr != nil {
					return saveErr
				}
			}
			return e
		}
		after.Observations[index] = publishing.DNSBatchObservation{State: "created", RecordID: actual.ID, RecordSHA256: dnsRecordSnapshot(actual)}
	}
	return m.saveBatch(ctx, t, before, after, "publication_dns_io_result")
}
func (m *publicCertificateExecution) failBatch(ctx context.Context, t tasks.Task, batch publishing.DNSBatch, result func(tasks.NodeResultCode, string) (map[string]tasks.WorkerResult, error), reason string) (map[string]tasks.WorkerResult, error) {
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	after := cloneDNSBatch(batch)
	after.State = "partial"
	for i, observed := range after.Observations {
		if observed.State == "creating" || observed.State == "deleting" || observed.State == "unknown" {
			after.Observations[i].State = "unknown"
			after.State = "unknown"
		}
	}
	if m.saveBatch(final, t, batch, after, "publication_dns_unresolved") != nil {
		return result(tasks.NodeResultUnknown, "DNS unresolved observation commit unknown")
	}
	code := tasks.NodeResultFailed
	if after.State == "unknown" {
		code = tasks.NodeResultUnknown
	}
	return result(code, reason)
}
func (m *publicCertificateExecution) dnsRecordLock(key string) *sync.Mutex {
	sum := sha256.Sum256([]byte(key))
	return &m.dnsRecordLocks[int(sum[0])%len(m.dnsRecordLocks)]
}
