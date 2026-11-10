package dns

import "context"

type ExecutionZoneBinding struct{ ZoneID, Name, AccountID, CredentialID string }

// ExecutionBinding is an owned read port. It exposes only an opaque credential
// ID and current synced zone, never provider token bytes or a raw account row.
func (s *Service) ExecutionBinding(ctx context.Context, subject, zoneID string) (ExecutionZoneBinding, error) {
	if s == nil || s.authorizeRead(ctx, subject, ActionZonesRead) != nil {
		return ExecutionZoneBinding{}, ErrProviderUnavailable
	}
	zone, account, err := s.zoneWithAccount(ctx, zoneID)
	if err != nil {
		return ExecutionZoneBinding{}, err
	}
	if _, _, err = s.credentialProvider(account); err != nil {
		return ExecutionZoneBinding{}, err
	}
	return ExecutionZoneBinding{ZoneID: zone.ID, Name: zone.Name, AccountID: account.ID, CredentialID: string(account.CredentialRef)}, nil
}

// AccountExecutionBinding also permits an unverified account so that the first
// credential check can run. It never grants provider access by itself.
func (s *Service) AccountExecutionBinding(ctx context.Context, subject, accountID string) (string, error) {
	if s == nil || s.authorizeRead(ctx, subject, ActionAccountsRead) != nil {
		return "", ErrProviderUnavailable
	}
	account, err := s.repo.getAccount(ctx, s.db, accountID)
	if err != nil {
		return "", err
	}
	if !account.Enabled || account.ProviderCode != ProviderCloudflare || account.CredentialRef == "" {
		return "", ErrCredentialNotConfigured
	}
	return string(account.CredentialRef), nil
}
