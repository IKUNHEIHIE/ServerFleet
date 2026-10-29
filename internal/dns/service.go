package dns

import (
	"context"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
)

const (
	ActionAccountsRead   = "dns.accounts.read"
	ActionZonesRead      = "dns.zones.read"
	ActionRecordsRead    = "dns.records.read"
	ActionAccountCreate  = "dns.account.create"
	ActionAccountUpdate  = "dns.account.update"
	ActionAccountTest    = "dns.account.test"
	ActionAccountDefault = "dns.account.default"
	ActionZoneCreate     = "dns.zone.create"
	ActionRecordCreate   = "dns.record.create"
	ActionRecordUpdate   = "dns.record.update"
	ActionRecordDelete   = "dns.record.delete"
)

// ServiceActions returns the audit action catalog this domain emits.
func ServiceActions() []string {
	return []string{
		ActionAccountCreate, ActionAccountDefault, ActionAccountTest, ActionAccountUpdate,
		ActionAccountsRead, ActionRecordCreate, ActionRecordDelete, ActionRecordUpdate,
		ActionRecordsRead, ActionZoneCreate, ActionZonesRead,
	}
}

// AuditSink is the durable audit port. Mutations write their event in the same
// transaction so an audit failure aborts the change.
type AuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}

// Options configures the DNS service. DB, Repo, Audit and Authorizer are
// required. A nil Provider leaves local account metadata usable while every
// provider-dependent operation fails closed.
type Options struct {
	DB         *storage.DB
	Repo       *Repo
	Audit      AuditSink
	Authorizer auth.Authorizer
	Provider   Provider
	Now        func() time.Time
}

type Service struct {
	db         *storage.DB
	repo       *Repo
	audit      AuditSink
	authorizer auth.Authorizer
	provider   Provider
	now        func() time.Time
}

func NewService(options Options) (*Service, error) {
	switch {
	case options.DB == nil:
		return nil, errors.New("dns: database is required")
	case options.Repo == nil:
		return nil, errors.New("dns: repository is required")
	case options.Audit == nil:
		return nil, errors.New("dns: audit sink is required")
	case options.Authorizer == nil:
		return nil, ErrMissingAuthorizer
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		db: options.DB, repo: options.Repo, audit: options.Audit,
		authorizer: options.Authorizer, provider: options.Provider, now: now,
	}, nil
}

func (s *Service) requireRequest(requestID string) error {
	if s == nil {
		return errors.New("dns: service is not configured")
	}
	if !validText(requestID, audit.DefaultLimits().MaxRequestIDBytes) {
		return ErrRequestIDRequired
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, subjectID, action, requestID string, nodeIDs []string, reason string) error {
	check := auth.Check{SubjectID: subjectID, Action: action, NodeIDs: append([]string(nil), nodeIDs...)}
	err := s.authorizer.Authorize(ctx, check)
	if err == nil {
		return nil
	}
	if isPureDenial(err) {
		denial := audit.Event{
			SubjectID: subjectID, Action: action, Result: "denied", RequestID: requestID,
			NodeIDs: append([]string(nil), nodeIDs...), Source: audit.SourceWeb,
			ReasonCode: reason, At: s.now(),
		}
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if auditErr := s.audit.Append(auditCtx, denial); auditErr != nil {
			return fmt.Errorf("dns: authorization denied (denial audit failed: %w)", auditErr)
		}
	}
	return err
}

func (s *Service) authorizeRead(ctx context.Context, subjectID, action string) error {
	if s == nil {
		return errors.New("dns: service is not configured")
	}
	return s.authorizer.Authorize(ctx, auth.Check{SubjectID: subjectID, Action: action})
}

func (s *Service) event(subjectID, action, result, requestID string, nodeIDs []string, reason string) audit.Event {
	return audit.Event{
		SubjectID: subjectID, Action: action, Result: result, RequestID: requestID,
		NodeIDs: append([]string(nil), nodeIDs...), Source: audit.SourceWeb,
		ReasonCode: reason, At: s.now(),
	}
}

func (s *Service) credentialProvider(account accountRecord) (Provider, CredentialRef, error) {
	if s.provider == nil {
		return nil, "", ErrProviderUnavailable
	}
	if !account.Enabled {
		return nil, "", ErrAccountDisabled
	}
	if account.CredentialRef == "" {
		return nil, "", ErrCredentialNotConfigured
	}
	if account.CredentialStatus != CredentialValid {
		return nil, "", ErrCredentialUnverified
	}
	return s.provider, account.CredentialRef, nil
}

func (s *Service) zoneWithAccount(ctx context.Context, zoneID string) (Zone, accountRecord, error) {
	zone, err := s.repo.getZone(ctx, s.db, zoneID)
	if err != nil {
		return Zone{}, accountRecord{}, err
	}
	account, err := s.repo.getAccount(ctx, s.db, zone.AccountID)
	if err != nil {
		return Zone{}, accountRecord{}, err
	}
	return zone, account, nil
}

// isPureDenial accepts only a single unwrap chain ending at auth.ErrDenied.
func isPureDenial(err error) bool {
	for depth := 0; err != nil; depth++ {
		if err == auth.ErrDenied {
			return true
		}
		if depth >= 64 {
			return false
		}
		if _, multi := err.(interface{ Unwrap() []error }); multi {
			return false
		}
		single, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = single.Unwrap()
	}
	return false
}
