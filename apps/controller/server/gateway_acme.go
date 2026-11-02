package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/certificates"
)

// ACMEDNS01Issuer is the ACME owner's DNS-01 issuance port. It returns the
// durable identities of every challenge record it published, including on error.
type ACMEDNS01Issuer interface {
	IssueDNS01(ctx context.Context, request certificates.ACMERequest) (certificates.ACMEIssued, error)
}

// ACMECleanupJournal is the durable cleanup journal seam. It is not a provider
// API and never receives credentials.
type ACMECleanupJournal interface {
	RecordFailure(ctx context.Context, subjectID, requestID, planID, domain string,
		records []certificates.ACMEDNS01Record) ([]certificates.CleanupRecord, error)
}

// PublicationACMEIssuer binds every ACME failure path to the publication plan,
// domain, FQDN, provider record id and expected value before returning. The
// journal write runs on a bounded context so caller cancellation cannot drop the
// only durable identity of a possibly leaked challenge record.
type PublicationACMEIssuer struct {
	Issuer  ACMEDNS01Issuer
	Cleanup ACMECleanupJournal
}

func (p *PublicationACMEIssuer) Issue(ctx context.Context, subjectID, requestID, planID, domain string,
	request certificates.ACMERequest) (certificates.ACMEIssued, error) {
	if p == nil || p.Issuer == nil || p.Cleanup == nil || planID == "" || domain == "" {
		return certificates.ACMEIssued{}, certificates.ErrCleanupUnavailable
	}
	issued, issueErr := p.Issuer.IssueDNS01(ctx, request)
	if issueErr == nil || len(issued.DNS01Records) == 0 {
		return issued, issueErr
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := p.Cleanup.RecordFailure(recordCtx, subjectID, requestID, planID, domain, issued.DNS01Records); err != nil {
		issueErr = errors.Join(issueErr, fmt.Errorf("certificates: persist ACME cleanup identity: %w", err))
	}
	return issued, issueErr
}
