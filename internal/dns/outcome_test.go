package dns_test

import (
	"context"
	"errors"
	"testing"

	"serverfleet.local/serverfleet/internal/dns"
)

func TestDeleteRecordKeepsLocalStateWhenProviderOutcomeIsUnknown(t *testing.T) {
	fixture := newACMEDNSFixture(t)
	record, err := fixture.service.CreateRecord(context.Background(), "admin", "req-create", "zone-1", dns.RecordInput{
		Type: dns.RecordTXT, Name: "_acme-challenge.app.example.com", Content: "value", TTL: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.provider.deleteErr = dns.ErrProviderOutcomeUnknown
	if err := fixture.service.DeleteRecord(context.Background(), "admin", "req-delete", record.ID); !errors.Is(err, dns.ErrProviderOutcomeUnknown) {
		t.Fatalf("delete err = %v, want ErrProviderOutcomeUnknown", err)
	}
	records, err := fixture.service.ListRecords(context.Background(), "admin", "zone-1")
	if err != nil || len(records) != 1 {
		t.Fatalf("local record was removed after an unknown provider outcome: %+v, %v", records, err)
	}
	fixture.provider.deleteErr = context.DeadlineExceeded
	if err := fixture.service.DeleteRecord(context.Background(), "admin", "req-delete", record.ID); !errors.Is(err, dns.ErrProviderOutcomeUnknown) {
		t.Fatalf("timeout err = %v, want ErrProviderOutcomeUnknown", err)
	}
	fixture.provider.deleteErr = nil
	if err := fixture.service.DeleteRecord(context.Background(), "admin", "req-delete", record.ID); err != nil {
		t.Fatal(err)
	}
	records, _ = fixture.service.ListRecords(context.Background(), "admin", "zone-1")
	if len(records) != 0 {
		t.Fatalf("record remained after confirmed delete: %+v", records)
	}
}

func TestDeleteRecordDefiniteFailureStaysRetryable(t *testing.T) {
	fixture := newACMEDNSFixture(t)
	record, err := fixture.service.CreateRecord(context.Background(), "admin", "req-create", "zone-1", dns.RecordInput{
		Type: dns.RecordTXT, Name: "_acme-challenge.app.example.com", Content: "value", TTL: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.provider.deleteErr = errors.New("provider rejected the request")
	if err := fixture.service.DeleteRecord(context.Background(), "admin", "req-delete", record.ID); !errors.Is(err, dns.ErrProviderFailed) {
		t.Fatalf("delete err = %v, want ErrProviderFailed", err)
	}
	fixture.provider.deleteErr = nil
	if err := fixture.service.DeleteRecord(context.Background(), "admin", "req-delete", record.ID); err != nil {
		t.Fatal(err)
	}
}

func TestACMEChallengeCleanupRetriesDefiniteFailureButNeverReplaysUnknownOutcome(t *testing.T) {
	fixture := newACMEDNSFixture(t)
	publisher, err := dns.NewACMEChallengePublisher(dns.ACMEPublisherConfig{
		Service: fixture.service, SubjectID: "acme", RequestID: "req-acme", ZoneID: "zone-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, cleanup, err := publisher.Publish(context.Background(), "_acme-challenge.app.example.com", "value")
	if err != nil {
		t.Fatal(err)
	}
	fixture.provider.deleteErr = dns.ErrProviderOutcomeUnknown
	if err := cleanup(context.Background()); !errors.Is(err, dns.ErrProviderOutcomeUnknown) {
		t.Fatalf("first cleanup err = %v, want ErrProviderOutcomeUnknown", err)
	}
	if err := cleanup(context.Background()); !errors.Is(err, dns.ErrProviderOutcomeUnknown) {
		t.Fatalf("second cleanup err = %v, want ErrProviderOutcomeUnknown", err)
	}
	if fixture.provider.deleteCalls != 1 {
		t.Fatalf("unknown outcome was replayed: deletes=%d", fixture.provider.deleteCalls)
	}
	records, _ := fixture.service.ListRecords(context.Background(), "admin", "zone-1")
	if len(records) != 1 {
		t.Fatalf("local record was removed after unknown outcome: %+v", records)
	}
}

func TestACMEChallengeCleanupRetriesConfirmedFailureAndBecomesIdempotent(t *testing.T) {
	fixture := newACMEDNSFixture(t)
	publisher, err := dns.NewACMEChallengePublisher(dns.ACMEPublisherConfig{
		Service: fixture.service, SubjectID: "acme", RequestID: "req-acme", ZoneID: "zone-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, cleanup, err := publisher.Publish(context.Background(), "_acme-challenge.app.example.com", "value")
	if err != nil {
		t.Fatal(err)
	}
	fixture.provider.deleteErr = errors.New("provider rejected the request")
	if err := cleanup(context.Background()); !errors.Is(err, dns.ErrProviderFailed) {
		t.Fatalf("first cleanup err = %v, want ErrProviderFailed", err)
	}
	fixture.provider.deleteErr = nil
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("retry cleanup err = %v", err)
	}
	if fixture.provider.deleteCalls != 2 {
		t.Fatalf("confirmed failure was not retried: deletes=%d", fixture.provider.deleteCalls)
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("idempotent cleanup err = %v", err)
	}
	if fixture.provider.deleteCalls != 2 {
		t.Fatalf("successful cleanup repeated the provider delete: %d", fixture.provider.deleteCalls)
	}
	records, _ := fixture.service.ListRecords(context.Background(), "admin", "zone-1")
	if len(records) != 0 {
		t.Fatalf("record remained after cleanup: %+v", records)
	}
}
