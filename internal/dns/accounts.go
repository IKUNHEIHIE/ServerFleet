package dns

import (
	"context"

	"serverfleet.local/serverfleet/internal/storage"
)

func (s *Service) ListAccounts(ctx context.Context, subjectID string) ([]AccountView, error) {
	if err := s.authorizeRead(ctx, subjectID, ActionAccountsRead); err != nil {
		return nil, err
	}
	records, err := s.repo.listAccounts(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]AccountView, 0, len(records))
	for _, record := range records {
		views = append(views, record.view())
	}
	return views, nil
}

func (s *Service) CreateAccount(ctx context.Context, subjectID, requestID string, input CreateAccountInput) (AccountView, error) {
	if err := s.requireRequest(requestID); err != nil {
		return AccountView{}, err
	}
	record, err := normalizeCreateAccountInput(input, s.now())
	if err != nil {
		return AccountView{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionAccountCreate, requestID, nil, "dns_account_create"); err != nil {
		return AccountView{}, err
	}
	var view AccountView
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if record.IsDefault {
			if _, err := tx.Exec(txCtx, `UPDATE dns_accounts SET is_default = 0
				WHERE provider_code = ? AND is_default = 1`, ProviderCloudflare); err != nil {
				return err
			}
		}
		if err := s.repo.createAccount(txCtx, tx, record); err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionAccountCreate, "success", requestID, nil, "")); err != nil {
			return err
		}
		view = record.view()
		return nil
	})
	return view, err
}

func (s *Service) SetDefault(ctx context.Context, subjectID, requestID, accountID string) (AccountView, error) {
	if err := s.requireRequest(requestID); err != nil {
		return AccountView{}, err
	}
	if !validID(accountID) {
		return AccountView{}, ErrAccountNotFound
	}
	if err := s.authorize(ctx, subjectID, ActionAccountDefault, requestID, nil, "dns_account_default"); err != nil {
		return AccountView{}, err
	}
	var view AccountView
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		record, err := s.repo.setDefault(txCtx, tx, accountID, s.now())
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionAccountDefault, "success", requestID, nil, "")); err != nil {
			return err
		}
		view = record.view()
		return nil
	})
	return view, err
}

func (s *Service) UpdateAccount(ctx context.Context, subjectID, requestID, accountID string, input UpdateAccountInput) (AccountView, error) {
	if err := s.requireRequest(requestID); err != nil {
		return AccountView{}, err
	}
	input, err := normalizeUpdateAccountInput(input)
	if err != nil {
		return AccountView{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionAccountUpdate, requestID, nil, "dns_account_update"); err != nil {
		return AccountView{}, err
	}
	var view AccountView
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		record, err := s.repo.getAccount(txCtx, tx, accountID)
		if err != nil {
			return err
		}
		record.DisplayName = input.DisplayName
		record.Note = input.Note
		record.Enabled = input.Enabled
		if input.Credential != "" {
			record.CredentialRef = input.Credential
			record.CredentialStatus = CredentialPendingVerification
		}
		record.UpdatedAt = s.now()
		if err := s.repo.updateAccount(txCtx, tx, record); err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionAccountUpdate, "success", requestID, nil, "")); err != nil {
			return err
		}
		view = record.view()
		return nil
	})
	return view, err
}

func (s *Service) TestAccount(ctx context.Context, subjectID, requestID, accountID string) (ConnectionTest, error) {
	if err := s.requireRequest(requestID); err != nil {
		return ConnectionTest{}, err
	}
	account, err := s.repo.getAccount(ctx, s.db, accountID)
	if err != nil {
		return ConnectionTest{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionAccountTest, requestID, nil, "dns_account_test"); err != nil {
		return ConnectionTest{}, err
	}
	if s.provider == nil {
		return ConnectionTest{}, ErrProviderUnavailable
	}
	if account.CredentialRef == "" {
		return ConnectionTest{}, ErrCredentialNotConfigured
	}
	result, err := s.provider.Test(WithSecretScope(ctx, SecretScope{SubjectID: subjectID, RequestID: requestID}), account.CredentialRef)
	if err != nil {
		return ConnectionTest{}, ErrProviderFailed
	}
	result = normalizeConnectionTest(result)
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, err := s.repo.updateCredentialStatus(txCtx, tx, accountID, result.Status, s.now()); err != nil {
			return err
		}
		auditResult := "success"
		if result.Status != CredentialValid {
			auditResult = "failure"
		}
		event := s.event(subjectID, ActionAccountTest, auditResult, requestID, nil,
			"credential_"+string(result.Status))
		if err := s.audit.AppendTx(txCtx, tx, event); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return ConnectionTest{}, err
	}
	return result, nil
}

func normalizeConnectionTest(result ConnectionTest) ConnectionTest {
	status := result.Status
	switch status {
	case CredentialValid, CredentialInvalid, CredentialMissingPermissions:
	default:
		status = CredentialInvalid
	}
	if result.OK && status != CredentialValid {
		status = CredentialInvalid
	}
	if !result.OK && status == CredentialValid {
		status = CredentialInvalid
	}
	return ConnectionTest{OK: status == CredentialValid, Status: status}
}
