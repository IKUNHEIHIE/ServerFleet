package dns

import (
	"context"

	"serverfleet.local/serverfleet/internal/storage"
)

func (s *Service) ListZones(ctx context.Context, subjectID, accountID string) ([]Zone, error) {
	if err := s.authorizeRead(ctx, subjectID, ActionZonesRead); err != nil {
		return nil, err
	}
	if accountID != "" && !validID(accountID) {
		return nil, ErrInvalidInput
	}
	return s.repo.listZones(ctx, accountID)
}

// CreateZone imports a zone after syncing it from the provider. The zone name and
// status always come from the provider response, never from the caller.
func (s *Service) CreateZone(ctx context.Context, subjectID, requestID, accountID string, input CreateZoneInput) (Zone, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Zone{}, err
	}
	if !validID(accountID) {
		return Zone{}, ErrAccountNotFound
	}
	if err := normalizeZoneInput(input); err != nil {
		return Zone{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionZoneCreate, requestID, nil, "dns_zone_create"); err != nil {
		return Zone{}, err
	}
	account, err := s.repo.getAccount(ctx, s.db, accountID)
	if err != nil {
		return Zone{}, err
	}
	provider, credential, err := s.credentialProvider(account)
	if err != nil {
		return Zone{}, err
	}
	infos, err := provider.ListZones(ctx, credential)
	if err != nil {
		return Zone{}, ErrProviderFailed
	}
	var info ZoneInfo
	found := false
	for _, candidate := range infos {
		if candidate.ProviderZoneID == input.ProviderZoneID {
			info = candidate
			found = true
			break
		}
	}
	if !found {
		return Zone{}, ErrZoneNotInProvider
	}
	info, status, err := normalizeProviderZone(info)
	if err != nil {
		return Zone{}, err
	}
	zone := Zone{
		ID: input.ID, AccountID: accountID, ProviderZoneID: info.ProviderZoneID,
		Name: info.Name, Status: status, SyncedAt: s.now(),
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.createZone(txCtx, tx, zone); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionZoneCreate, "success", requestID, nil, ""))
	})
	if err != nil {
		return Zone{}, err
	}
	return zone, nil
}
