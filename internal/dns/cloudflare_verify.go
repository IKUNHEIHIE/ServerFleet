package dns

import (
	"context"
	"net/http"
	"net/url"
)

func (p *CloudflareProvider) VerifyRecord(ctx context.Context, credential CredentialRef, zoneID, recordID string, expected RecordInput) error {
	if !validProviderID(zoneID) || !validProviderID(recordID) || ValidateExecutionRecord(expected) != nil {
		return ErrInvalidInput
	}
	response, err := p.doRecord(ctx, http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), credential, nil, "read record")
	if err != nil {
		return err
	}
	actual := response.Result
	if actual.ID != recordID || actual.Type != string(expected.Type) || actual.Name != expected.Name || actual.Content != expected.Content || actual.Proxied == nil || *actual.Proxied != expected.Proxied {
		return ErrProviderRecordChanged
	}
	if expected.Type == RecordMX && (expected.Priority == nil || actual.Priority == nil || *expected.Priority != *actual.Priority) {
		return ErrProviderRecordChanged
	}
	return nil
}
func (p *CloudflareProvider) DeleteVerifiedRecord(ctx context.Context, credential CredentialRef, zoneID, recordID string, expected RecordInput) error {
	if err := p.VerifyRecord(ctx, credential, zoneID, recordID, expected); err != nil {
		return err
	}
	// No undocumented If-Match/CAS header is invented. Every request still checks
	// the live Controller task, owner, approval, claim and frozen local identity.
	return p.DeleteRecord(ctx, credential, zoneID, recordID)
}
