package certificates

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
	ActionCertificateRead    = "certificates.read"
	ActionDistributionCreate = "certificates.distribution.create"
	ActionCertificateRenew   = "certificates.renew"
	ActionCertificateRevoke  = "certificates.revoke"
	ActionCertificateRotate  = "certificates.rotate"
)

// ServiceActions returns the audit action catalog this domain emits.
func ServiceActions() []string {
	return []string{
		ActionCertificateRead, ActionCertificateRenew, ActionCertificateRevoke,
		ActionCertificateRotate, ActionDistributionCreate,
	}
}

// AuditSink is the durable audit port. Mutations write their event in the same
// transaction so an audit failure aborts the change.
type AuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}

// Options configures the certificate service. DB, Repo, Audit and Authorizer are
// required. A nil Issuer, TaskSubmitter or GroupRevisions makes the corresponding
// operation fail closed.
type Options struct {
	DB             *storage.DB
	Repo           *Repo
	Audit          AuditSink
	Authorizer     auth.Authorizer
	Issuer         Issuer
	Tasks          TaskSubmitter
	GroupRevisions GroupRevisions
	Now            func() time.Time
}

type Service struct {
	db             *storage.DB
	repo           *Repo
	audit          AuditSink
	authorizer     auth.Authorizer
	issuer         Issuer
	tasks          TaskSubmitter
	groupRevisions GroupRevisions
	now            func() time.Time
}

func NewService(options Options) (*Service, error) {
	switch {
	case options.DB == nil:
		return nil, errors.New("certificates: database is required")
	case options.Repo == nil:
		return nil, errors.New("certificates: repository is required")
	case options.Audit == nil:
		return nil, errors.New("certificates: audit sink is required")
	case options.Authorizer == nil:
		return nil, ErrMissingAuthorizer
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		db: options.DB, repo: options.Repo, audit: options.Audit,
		authorizer: options.Authorizer, issuer: options.Issuer, tasks: options.Tasks,
		groupRevisions: options.GroupRevisions, now: now,
	}, nil
}

func (s *Service) requireRequest(requestID string) error {
	if s == nil {
		return errors.New("certificates: service is not configured")
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
			return fmt.Errorf("certificates: authorization denied (denial audit failed: %w)", auditErr)
		}
	}
	return err
}

func (s *Service) authorizeRead(ctx context.Context, subjectID string, nodeIDs []string) error {
	if s == nil {
		return errors.New("certificates: service is not configured")
	}
	return s.authorizer.Authorize(ctx, auth.Check{
		SubjectID: subjectID, Action: ActionCertificateRead,
		NodeIDs: append([]string(nil), nodeIDs...),
	})
}

func (s *Service) event(subjectID, action, result, requestID, taskID string, nodeIDs []string, reason string) audit.Event {
	return audit.Event{
		SubjectID: subjectID, Action: action, Result: result, RequestID: requestID,
		TaskID: taskID, NodeIDs: append([]string(nil), nodeIDs...), Source: audit.SourceWeb,
		ReasonCode: reason, At: s.now(),
	}
}

func (s *Service) certificateNodes(ctx context.Context, certificateID string) ([]string, error) {
	return s.repo.nodeIDs(ctx, certificateID)
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
