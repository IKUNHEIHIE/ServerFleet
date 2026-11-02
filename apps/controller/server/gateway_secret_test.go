package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/publishing"
)

type reviewCredentialLookup struct {
	meta ProviderCredential
	ok   bool
	err  error
}

func (l reviewCredentialLookup) LookupProviderCredential(context.Context, string) (ProviderCredential, bool, error) {
	return l.meta, l.ok, l.err
}

type reviewCredentialUse struct {
	mu     sync.Mutex
	ops    []auth.CredentialOperation
	calls  int
	secret string
	err    error
	zeroed bool
}

func (c *reviewCredentialUse) UseForService(ctx context.Context, op auth.CredentialOperation, consume func(context.Context, []byte) error) error {
	c.mu.Lock()
	c.calls++
	c.ops = append(c.ops, op)
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	plain := []byte(c.secret)
	c.mu.Unlock()
	consumeErr := consume(ctx, plain)
	zeroed := true
	for _, b := range plain {
		if b != 0 {
			zeroed = false
			break
		}
	}
	c.mu.Lock()
	c.zeroed = zeroed
	c.mu.Unlock()
	if consumeErr != nil {
		return auth.ErrCredentialUseUnknown
	}
	return nil
}

func (c *reviewCredentialUse) snapshot() ([]auth.CredentialOperation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]auth.CredentialOperation(nil), c.ops...), c.zeroed
}

func reviewProviderMetadata() ProviderCredential {
	return ProviderCredential{
		CredentialID: "cred-1", Owner: "owner", Module: "dns", Purpose: "dns-provider", Revision: 3,
	}
}

func TestPublicationSecretBorrowerBindsOwnerRevisionPurposeAndPlan(t *testing.T) {
	credentials := &reviewCredentialUse{secret: "synthetic-dns-token"}
	borrower, err := NewDNSPublisherSecretBorrower(credentials, reviewCredentialLookup{meta: reviewProviderMetadata(), ok: true})
	if err != nil {
		t.Fatal(err)
	}
	trusted := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-dns-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer trusted.Close()
	planDigest := strings.Repeat("a", 64)
	use := dns.SecretUse{
		Ref: "ref-1", Operation: "dns.ensure_record", SubjectID: "owner", RequestID: "req-dns",
		PlanDigest: planDigest, Module: "dns", Purpose: "dns-provider",
	}
	var status int
	err = borrower.WithSecret(context.Background(), use, func(ctx context.Context, secret string) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, trusted.URL, nil)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+secret)
		response, err := trusted.Client().Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		status = response.StatusCode
		return nil
	})
	if err != nil || status != http.StatusOK {
		t.Fatalf("normal TLS call = %d, %v", status, err)
	}
	ops, zeroed := credentials.snapshot()
	if len(ops) != 1 || ops[0].SubjectID != "owner" || ops[0].CredentialID != "cred-1" ||
		ops[0].Module != "dns" || ops[0].Source != "system" || ops[0].Action != "use" ||
		ops[0].ExpectedRevision != 3 || ops[0].PlanDigest != planDigest ||
		!strings.HasPrefix(ops[0].OperationID, "dns.ensure_record-") {
		t.Fatalf("operation = %+v", ops)
	}
	if !zeroed {
		t.Fatal("borrowed secret was not cleared")
	}
}

func TestPublicationSecretBorrowerTLSFailureIsNotSwallowed(t *testing.T) {
	credentials := &reviewCredentialUse{secret: "synthetic-dns-token"}
	borrower, err := NewDNSPublisherSecretBorrower(credentials, reviewCredentialLookup{meta: reviewProviderMetadata(), ok: true})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("must not be reached over unknown TLS"))
	}))
	defer server.Close()
	untrusted := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: x509.NewCertPool(),
	}}}
	err = borrower.WithSecret(context.Background(), dns.SecretUse{
		Ref: "ref-1", Operation: "dns.delete_record", SubjectID: "owner", RequestID: "req-dns",
		PlanDigest: strings.Repeat("b", 64), Module: "dns", Purpose: "dns-provider",
	}, func(ctx context.Context, secret string) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+secret)
		response, err := untrusted.Do(request)
		if err != nil {
			return err
		}
		_ = response.Body.Close()
		return nil
	})
	if !errors.Is(err, auth.ErrCredentialUseUnknown) {
		t.Fatalf("TLS failure err = %v", err)
	}
	if _, zeroed := credentials.snapshot(); !zeroed {
		t.Fatal("secret survived a failed TLS call")
	}
}

func TestPublicationSecretBorrowerRefusesMissingOrWrongPurpose(t *testing.T) {
	cases := []struct {
		name   string
		lookup reviewCredentialLookup
		use    dns.SecretUse
	}{
		{"missing", reviewCredentialLookup{}, dns.SecretUse{Ref: "ref-1", Operation: "dns.ensure", Module: "dns", Purpose: "dns-provider", PlanDigest: strings.Repeat("a", 64), SubjectID: "owner", RequestID: "req"}},
		{"wrong-purpose", reviewCredentialLookup{meta: ProviderCredential{CredentialID: "cred-1", Owner: "owner", Module: "dns", Purpose: "smtp-password", Revision: 1}, ok: true}, dns.SecretUse{Ref: "ref-1", Operation: "dns.ensure", Module: "dns", Purpose: "dns-provider", PlanDigest: strings.Repeat("a", 64), SubjectID: "owner", RequestID: "req"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			credentials := &reviewCredentialUse{secret: "must-not-be-used"}
			borrower, err := NewDNSPublisherSecretBorrower(credentials, test.lookup)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			err = borrower.WithSecret(context.Background(), test.use, func(context.Context, string) error {
				called = true
				return nil
			})
			if !errors.Is(err, publishing.ErrProductionUnavailable) || called {
				t.Fatalf("err = %v, callback called = %v", err, called)
			}
			if ops, _ := credentials.snapshot(); len(ops) != 0 {
				t.Fatalf("credential service was called: %+v", ops)
			}
		})
	}
}

func TestNotificationBorrowerRequiresChannelPurpose(t *testing.T) {
	credentials := &reviewCredentialUse{secret: "synthetic-smtp-password"}
	borrower, err := NewNotificationSecretBorrower(credentials, reviewCredentialLookup{meta: ProviderCredential{
		CredentialID: "cred-2", Owner: "owner", Module: "qqbot", Purpose: "qqbot-secret", Revision: 2,
	}, ok: true})
	if err != nil {
		t.Fatal(err)
	}
	err = borrower.WithSecret(context.Background(), notifications.SecretUse{
		Ref: "ref-2", Operation: "smtp.send", SubjectID: "owner", RequestID: "req-mail",
		PlanDigest: strings.Repeat("c", 64), Module: "smtp", Purpose: "smtp-password",
	}, func(context.Context, string) error {
		return nil
	})
	if !errors.Is(err, publishing.ErrProductionUnavailable) {
		t.Fatalf("cross-purpose borrow err = %v", err)
	}
}

type reviewACMEIssuer struct {
	issued certificates.ACMEIssued
	err    error
	calls  int
}

func (i *reviewACMEIssuer) IssueDNS01(context.Context, certificates.ACMERequest) (certificates.ACMEIssued, error) {
	i.calls++
	return i.issued, i.err
}

type reviewCleanupJournal struct {
	mu         sync.Mutex
	calls      int
	subject    string
	request    string
	plan       string
	domain     string
	records    []certificates.ACMEDNS01Record
	err        error
	ctxErrored bool
}

func (j *reviewCleanupJournal) RecordFailure(ctx context.Context, subjectID, requestID, planID, domain string, records []certificates.ACMEDNS01Record) ([]certificates.CleanupRecord, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.calls++
	j.subject, j.request, j.plan, j.domain = subjectID, requestID, planID, domain
	j.records = append([]certificates.ACMEDNS01Record(nil), records...)
	j.ctxErrored = ctx.Err() != nil
	return nil, j.err
}

func TestPublicationACMEIssuerRecordsFailureBoundToPlanAndRecords(t *testing.T) {
	records := []certificates.ACMEDNS01Record{
		{FQDN: "_acme-challenge.app.example.com", RecordID: "record-1", Value: "txt-1"},
	}
	issuer := &reviewACMEIssuer{
		issued: certificates.ACMEIssued{DNS01Records: records},
		err:    certificates.ErrACMEIssuance,
	}
	journal := &reviewCleanupJournal{}
	publication := &PublicationACMEIssuer{Issuer: issuer, Cleanup: journal}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := publication.Issue(canceled, "owner", "req-acme", "plan-1", "app.example.com",
		certificates.ACMERequest{CommonName: "app.example.com", KeyRef: "key-1"})
	if !errors.Is(err, certificates.ErrACMEIssuance) {
		t.Fatalf("err = %v", err)
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.calls != 1 || journal.subject != "owner" || journal.request != "req-acme" ||
		journal.plan != "plan-1" || journal.domain != "app.example.com" ||
		len(journal.records) != 1 || journal.records[0].FQDN != records[0].FQDN ||
		journal.records[0].RecordID != "record-1" || journal.records[0].Value != "txt-1" {
		t.Fatalf("journal = %+v", journal)
	}
	if journal.ctxErrored {
		t.Fatal("cleanup identity write used a canceled context")
	}
}

func TestPublicationACMEIssuerSkipsJournalOnCleanSuccess(t *testing.T) {
	issuer := &reviewACMEIssuer{issued: certificates.ACMEIssued{
		DNS01Records: []certificates.ACMEDNS01Record{{FQDN: "_acme-challenge.app.example.com", RecordID: "record-1", Value: "txt-1"}},
	}}
	journal := &reviewCleanupJournal{}
	publication := &PublicationACMEIssuer{Issuer: issuer, Cleanup: journal}
	if _, err := publication.Issue(context.Background(), "owner", "req-acme", "plan-1", "app.example.com",
		certificates.ACMERequest{CommonName: "app.example.com", KeyRef: "key-1"}); err != nil {
		t.Fatal(err)
	}
	if journal.calls != 0 {
		t.Fatalf("clean success recorded failure cleanup: %d", journal.calls)
	}
}

func TestPublicationACMEIssuerSurfacesJournalFailure(t *testing.T) {
	issuer := &reviewACMEIssuer{
		issued: certificates.ACMEIssued{DNS01Records: []certificates.ACMEDNS01Record{
			{FQDN: "_acme-challenge.app.example.com", RecordID: "record-1", Value: "txt-1"},
		}},
		err: fmt.Errorf("%w: challenge timeout", certificates.ErrACMEChallenge),
	}
	journal := &reviewCleanupJournal{err: errors.New("journal unavailable")}
	publication := &PublicationACMEIssuer{Issuer: issuer, Cleanup: journal}
	_, err := publication.Issue(context.Background(), "owner", "req-acme", "plan-1", "app.example.com",
		certificates.ACMERequest{CommonName: "app.example.com", KeyRef: "key-1"})
	if !errors.Is(err, certificates.ErrACMEChallenge) || !strings.Contains(err.Error(), "journal unavailable") {
		t.Fatalf("err = %v", err)
	}
}
