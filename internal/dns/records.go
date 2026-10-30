package dns

import (
	"context"

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
	providerID, err := provider.CreateRecord(ctx, credential, zone.ProviderZoneID, input)
	if err != nil {
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
		return Record{}, err
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
	if err := provider.UpdateRecord(ctx, credential, zone.ProviderZoneID, current.ID, input); err != nil {
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
		return Record{}, err
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
	if err := provider.DeleteRecord(ctx, credential, zone.ProviderZoneID, current.ID); err != nil {
		return ErrProviderFailed
	}
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.deleteRecord(txCtx, tx, recordID); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRecordDelete, "success", requestID, nil, ""))
	})
}
