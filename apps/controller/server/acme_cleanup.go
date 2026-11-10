package server

import (
	"context"

	"serverfleet.local/serverfleet/internal/certificates"
)

type publicCertificateCleaner struct {
	manager *publicCertificateExecution
	next    certificates.DNSRecordCleaner
}

func (c publicCertificateCleaner) DeleteChallengeRecord(ctx context.Context, subject, requestID, fqdn, recordID, value string) error {
	m := c.manager
	if m == nil || m.s.CertificateCleanup == nil || c.next == nil {
		return certificates.ErrCleanupUnavailable
	}
	record, err := m.s.CertificateCleanup.CurrentExecution(ctx)
	if err != nil || record.SubjectID != subject || record.FQDN != fqdn || record.RecordID != recordID || record.RecordValue != value {
		return certificates.ErrCleanupBusy
	}
	task, err := m.s.TasksRepo.Get(ctx, record.PlanID)
	if err != nil {
		return certificates.ErrCleanupUnavailable
	}
	csrID, zoneID, err := m.requestParams(task)
	if err != nil {
		return certificates.ErrCleanupUnavailable
	}
	order, err := certificates.ReadPublicOrder(ctx, m.s.DB, task.ID)
	if err != nil || order.CSRTaskID != csrID || order.ZoneID != zoneID || order.PlanSHA256 != task.PlanHash || order.ProfileSHA256 != m.profileSHA || order.Domain != record.Domain {
		return certificates.ErrCleanupUnavailable
	}
	scope := m.taskScope(task, zoneID)
	scope.cleanup = true
	scope.cleanupID = record.ID
	scope.requestID = requestID
	scoped := m.scope(ctx, scope)
	if m.current(scoped, scope) != nil || m.zone(scoped, task, zoneID, record.Domain) != nil {
		return certificates.ErrCleanupUnavailable
	}
	return c.next.DeleteChallengeRecord(scoped, subject, requestID, fqdn, recordID, value)
}
