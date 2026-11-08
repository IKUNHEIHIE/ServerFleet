package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

func (s *Service) ListRecords(ctx context.Context, subjectID, zoneID string) ([]Record, error) {
	if err := s.authorizeRead(ctx, subjectID, ActionRecordsRead); err != nil {
		return nil, err
	}
	if zoneID != "" && !validID(zoneID) {
		return nil, ErrInvalidInput
	}
	return s.repo.listRecords(ctx, zoneID)
}

func (s *Service) CreateRecord(ctx context.Context, subjectID, requestID, zoneID string, input RecordInput) (Record, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Record{}, err
	}
	if !validID(zoneID) {
		return Record{}, ErrZoneNotFound
	}
	priority, err := validateRecordInput(input)
	if err != nil {
		return Record{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionRecordCreate, requestID, nil, "dns_record_create"); err != nil {
		return Record{}, err
	}
	zone, account, err := s.zoneWithAccount(ctx, zoneID)
	if err != nil {
		return Record{}, err
	}
	provider, credential, err := s.credentialProvider(account)
	if err != nil {
		return Record{}, err
	}
	providerID, err := provider.CreateRecord(WithSecretScope(ctx, SecretScope{SubjectID: subjectID, RequestID: requestID}), credential, zone.ProviderZoneID, input)
	if err != nil {
		if providerOutcomeUnknown(err) {
			return Record{}, fmt.Errorf("%w: %w", ErrProviderOutcomeUnknown, err)
		}
		return Record{}, ErrProviderFailed
	}
	if !validProviderID(providerID) {
		return Record{}, ErrInvalidProviderResponse
	}
	record := Record{
		ID: providerID, ZoneID: zoneID, Type: input.Type, Name: input.Name,
		Content: input.Content, TTL: input.TTL, Proxied: input.Proxied,
		Priority: priority, ModifiedAt: s.now(),
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.createRecord(txCtx, tx, record); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRecordCreate, "success", requestID, nil, ""))
	})
	if err != nil {
		compCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		compCtx = WithSecretScope(compCtx, SecretScope{SubjectID: subjectID, RequestID: requestID})
		compErr := provider.DeleteRecord(compCtx, credential, zone.ProviderZoneID, providerID)
		cancel()
		if compErr != nil {
			return Record{}, fmt.Errorf("dns: persist record failed and provider cleanup did not complete: %v", err)
		}
		return Record{}, fmt.Errorf("dns: persist record: %w", err)
	}
	return record, nil
}

func (s *Service) UpdateRecord(ctx context.Context, subjectID, requestID, recordID string, input RecordInput) (Record, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Record{}, err
	}
	priority, err := validateRecordInput(input)
	if err != nil {
		return Record{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionRecordUpdate, requestID, nil, "dns_record_update"); err != nil {
		return Record{}, err
	}
	current, err := s.repo.getRecord(ctx, s.db, recordID)
	if err != nil {
		return Record{}, err
	}
	zone, account, err := s.zoneWithAccount(ctx, current.ZoneID)
	if err != nil {
		return Record{}, err
	}
	provider, credential, err := s.credentialProvider(account)
	if err != nil {
		return Record{}, err
	}
	if err := provider.UpdateRecord(WithSecretScope(ctx, SecretScope{SubjectID: subjectID, RequestID: requestID}), credential, zone.ProviderZoneID, current.ID, input); err != nil {
		if providerOutcomeUnknown(err) {
			return Record{}, fmt.Errorf("%w: %w", ErrProviderOutcomeUnknown, err)
		}
		return Record{}, ErrProviderFailed
	}
	updated := Record{
		ID: current.ID, ZoneID: current.ZoneID, Type: input.Type, Name: input.Name,
		Content: input.Content, TTL: input.TTL, Proxied: input.Proxied,
		Priority: priority, ModifiedAt: s.now(),
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.updateRecord(txCtx, tx, updated); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRecordUpdate, "success", requestID, nil, ""))
	})
	if err != nil {
		compCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		compCtx = WithSecretScope(compCtx, SecretScope{SubjectID: subjectID, RequestID: requestID})
		compErr := provider.UpdateRecord(compCtx, credential, zone.ProviderZoneID, current.ID, recordInputFrom(current))
		cancel()
		if compErr != nil {
			return Record{}, fmt.Errorf("dns: persist record update failed and provider rollback did not complete: %v", err)
		}
		return Record{}, fmt.Errorf("dns: persist record update: %w", err)
	}
	return updated, nil
}

func (s *Service) DeleteRecord(ctx context.Context, subjectID, requestID, recordID string) error {
	if err := s.requireRequest(requestID); err != nil {
		return err
	}
	if err := s.authorize(ctx, subjectID, ActionRecordDelete, requestID, nil, "dns_record_delete"); err != nil {
		return err
	}
	current, err := s.repo.getRecord(ctx, s.db, recordID)
	if err != nil {
		return err
	}
	zone, account, err := s.zoneWithAccount(ctx, current.ZoneID)
	if err != nil {
		return err
	}
	provider, credential, err := s.credentialProvider(account)
	if err != nil {
		return err
	}
	providerAbsent := false
	if err := provider.DeleteRecord(WithSecretScope(ctx, SecretScope{SubjectID: subjectID, RequestID: requestID}), credential, zone.ProviderZoneID, current.ID); err != nil {
		if !errors.Is(err, ErrProviderNotFound) {
			if providerOutcomeUnknown(err) {
				return fmt.Errorf("%w: %w", ErrProviderOutcomeUnknown, err)
			}
			return ErrProviderFailed
		}
		providerAbsent = true
	}
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.deleteRecord(txCtx, tx, recordID); err != nil {
			return err
		}
		reason := ""
		if providerAbsent {
			reason = "provider_record_absent"
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRecordDelete, "success", requestID, nil, reason))
	})
}

func recordInputFrom(record Record) RecordInput {
	input := RecordInput{
		Type: record.Type, Name: record.Name, Content: record.Content,
		TTL: record.TTL, Proxied: record.Proxied,
	}
	if record.Type == RecordMX {
		priority := record.Priority
		input.Priority = &priority
	}
	return input
}

// providerOutcomeUnknown classifies a provider failure whose remote effect is not
// proven. Transport errors, timeouts and cancellation may have reached the
// provider, so a delete/create must not be replayed automatically.
func providerOutcomeUnknown(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrProviderOutcomeUnknown) || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrCloudflareTransport) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}
