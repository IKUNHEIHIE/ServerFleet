package dns

import (
	"context"
	"errors"
)

var ErrProviderRecordChanged = errors.New("dns: provider record identity or value changed")

// RecordVerifier reads the exact provider ID before a managed reuse/deletion.
// It is a preflight observation, NOT a provider-supported atomic CAS promise.
type RecordVerifier interface {
	VerifyRecord(context.Context, CredentialRef, string, string, RecordInput) error
}
type CheckedRecordDeleter interface {
	DeleteVerifiedRecord(context.Context, CredentialRef, string, string, RecordInput) error
}

// VerifyExecutionRecord uses only this owner's synced zone/account and current
// local row. Provider credentials still require the caller's private task scope.
func (s *Service) VerifyExecutionRecord(ctx context.Context, subject, request, zoneID, recordID string) error {
	if err := s.requireRequest(request); err != nil {
		return err
	}
	if err := s.authorize(ctx, subject, ActionRecordsRead, request, nil, "dns_provider_record_verify"); err != nil {
		return err
	}
	record, err := s.repo.getRecord(ctx, s.db, recordID)
	if err != nil || record.ZoneID != zoneID {
		return ErrRecordNotFound
	}
	zone, account, err := s.zoneWithAccount(ctx, zoneID)
	if err != nil {
		return err
	}
	provider, credential, err := s.credentialProvider(account)
	if err != nil {
		return err
	}
	verifier, ok := provider.(RecordVerifier)
	if !ok {
		return ErrProviderUnavailable
	}
	return verifier.VerifyRecord(WithSecretScope(ctx, SecretScope{SubjectID: subject, RequestID: request}), credential, zone.ProviderZoneID, record.ID, recordInputFrom(record))
}
