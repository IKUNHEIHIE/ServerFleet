package dns_test

import (
	"context"
	"errors"
	"testing"

	"serverfleet.local/serverfleet/internal/dns"
)

func newACMEDNSFixture(t *testing.T) dnsFixture {
	t.Helper()
	fixture := newDNSFixture(t, nil, newFakeProvider(), nil)
	account, err := fixture.service.CreateAccount(context.Background(), "admin", "req-account", dns.CreateAccountInput{
		ID: "account-1", DisplayName: "Cloudflare", Enabled: true, MakeDefault: true, Credential: "secret/cf-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.TestAccount(context.Background(), "admin", "req-test", account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.CreateZone(context.Background(), "admin", "req-zone", account.ID, dns.CreateZoneInput{
		ID: "zone-1", ProviderZoneID: "pz-1",
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestACMEChallengePublisherCreatesAndRemovesTXT(t *testing.T) {
	fixture := newACMEDNSFixture(t)
	publisher, err := dns.NewACMEChallengePublisher(dns.ACMEPublisherConfig{
		Service: fixture.service, SubjectID: "acme", RequestID: "req-acme", ZoneID: "zone-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := publisher.Publish(context.Background(), "_acme-challenge.app.example.com", "dns01-value")
	if err != nil {
		t.Fatal(err)
	}
	records, err := fixture.service.ListRecords(context.Background(), "admin", "zone-1")
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %+v, %v", records, err)
	}
	if records[0].Type != dns.RecordTXT || records[0].Name != "_acme-challenge.app.example.com" ||
		records[0].Content != "dns01-value" || records[0].TTL != 60 {
		t.Fatalf("record = %+v", records[0])
	}
	if fixture.provider.createCalls != 1 {
		t.Fatalf("provider creates = %d", fixture.provider.createCalls)
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fixture.provider.deleteCalls != 1 {
		t.Fatalf("provider deletes = %d", fixture.provider.deleteCalls)
	}
	records, _ = fixture.service.ListRecords(context.Background(), "admin", "zone-1")
	if len(records) != 0 {
		t.Fatalf("records after cleanup = %+v", records)
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("second cleanup = %v", err)
	}
	if fixture.provider.deleteCalls != 1 {
		t.Fatal("cleanup repeated the provider delete")
	}
}

func TestACMEChallengePublisherTreatsProviderNotFoundAsCleanup(t *testing.T) {
	fixture := newACMEDNSFixture(t)
	fixture.provider.deleteErr = dns.ErrProviderNotFound
	publisher, err := dns.NewACMEChallengePublisher(dns.ACMEPublisherConfig{
		Service: fixture.service, SubjectID: "acme", RequestID: "req-acme", ZoneID: "zone-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := publisher.Publish(context.Background(), "_acme-challenge.app.example.com", "value")
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup = %v", err)
	}
	records, _ := fixture.service.ListRecords(context.Background(), "admin", "zone-1")
	if len(records) != 0 {
		t.Fatalf("local record remained after provider 404: %+v", records)
	}
}

func TestACMEChallengePublisherValidation(t *testing.T) {
	fixture := newACMEDNSFixture(t)
	if _, err := dns.NewACMEChallengePublisher(dns.ACMEPublisherConfig{}); !errors.Is(err, dns.ErrInvalidInput) {
		t.Fatalf("empty config err = %v", err)
	}
	if _, err := dns.NewACMEChallengePublisher(dns.ACMEPublisherConfig{
		Service: fixture.service, SubjectID: "acme", RequestID: "req", ZoneID: "zone-1", TTL: 5,
	}); !errors.Is(err, dns.ErrInvalidInput) {
		t.Fatalf("ttl err = %v", err)
	}
	publisher, err := dns.NewACMEChallengePublisher(dns.ACMEPublisherConfig{
		Service: fixture.service, SubjectID: "acme", RequestID: "req", ZoneID: "zone-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(context.Background(), "app.example.com", "value"); err == nil {
		t.Fatal("non-challenge name accepted")
	}
	if _, err := publisher.Publish(context.Background(), "_acme-challenge.app.example.com", "bad value"); err == nil {
		t.Fatal("value with whitespace accepted")
	}
	fixture.provider.createErr = errors.New("provider down")
	if _, err := publisher.Publish(context.Background(), "_acme-challenge.app.example.com", "value"); !errors.Is(err, dns.ErrProviderFailed) {
		t.Fatalf("provider err = %v", err)
	}
}
