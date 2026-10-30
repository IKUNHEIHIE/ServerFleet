package dns_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type authorizerFunc func(context.Context, auth.Check) error

func (f authorizerFunc) Authorize(ctx context.Context, check auth.Check) error {
	return f(ctx, check)
}

type fakeProvider struct {
	zones       []dns.ZoneInfo
	test        dns.ConnectionTest
	testErr     error
	zoneErr     error
	createErr   error
	createCalls int
	updateCalls int
	deleteCalls int
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		zones: []dns.ZoneInfo{{ProviderZoneID: "pz-1", Name: "example.com", Status: "active"}},
		test:  dns.ConnectionTest{OK: true, Status: dns.CredentialValid},
	}
}

func (p *fakeProvider) Test(context.Context, dns.CredentialRef) (dns.ConnectionTest, error) {
	if p.testErr != nil {
		return dns.ConnectionTest{}, p.testErr
	}
	return p.test, nil
}

func (p *fakeProvider) ListZones(context.Context, dns.CredentialRef) ([]dns.ZoneInfo, error) {
	if p.zoneErr != nil {
		return nil, p.zoneErr
	}
	return append([]dns.ZoneInfo(nil), p.zones...), nil
}

func (p *fakeProvider) CreateRecord(context.Context, dns.CredentialRef, string, dns.RecordInput) (string, error) {
	p.createCalls++
	if p.createErr != nil {
		return "", p.createErr
	}
	return fmt.Sprintf("rec-%d", p.createCalls), nil
}

func (p *fakeProvider) UpdateRecord(context.Context, dns.CredentialRef, string, string, dns.RecordInput) error {
	p.updateCalls++
	return nil
}

func (p *fakeProvider) DeleteRecord(context.Context, dns.CredentialRef, string, string) error {
	p.deleteCalls++
	return nil
}

type dnsFixture struct {
	db       *storage.DB
	repo     *dns.Repo
	audit    *audit.SQLRecorder
	service  *dns.Service
	provider *fakeProvider
	path     string
}

func newDNSFixture(t *testing.T, authorizer auth.Authorizer, provider dns.Provider, actions []string) dnsFixture {
	t.Helper()
	return newDNSFixtureAt(t, filepath.Join(t.TempDir(), "fleet.db"), authorizer, provider, actions)
}

func newDNSFixtureAt(t *testing.T, path string, authorizer auth.Authorizer, provider dns.Provider, actions []string) dnsFixture {
	t.Helper()
	db := testkit.NewDBAt(t, path, dns.Migrations(), audit.Migrations())
	repo, err := dns.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	if actions == nil {
		actions = dns.ServiceActions()
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(actions, limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if authorizer == nil {
		authorizer = authorizerFunc(func(context.Context, auth.Check) error { return nil })
	}
	service, err := dns.NewService(dns.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: authorizer, Provider: provider,
	})
	if err != nil {
		t.Fatal(err)
	}
	fake, _ := provider.(*fakeProvider)
	return dnsFixture{db: db, repo: repo, audit: recorder, service: service, provider: fake, path: path}
}

func actionsExcept(exclude ...string) []string {
	skip := make(map[string]struct{}, len(exclude))
	for _, action := range exclude {
		skip[action] = struct{}{}
	}
	out := make([]string, 0)
	for _, action := range dns.ServiceActions() {
		if _, omitted := skip[action]; !omitted {
			out = append(out, action)
		}
	}
	return out
}

func (f dnsFixture) readyAccount(t *testing.T, id string) dns.AccountView {
	t.Helper()
	ctx := context.Background()
	view, err := f.service.CreateAccount(ctx, "admin", "req-create-"+id, dns.CreateAccountInput{
		ID: id, DisplayName: id, Enabled: true,
		Credential: dns.CredentialRef("ref-" + id),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.TestAccount(ctx, "admin", "req-test-"+id, id); err != nil {
		t.Fatal(err)
	}
	return view
}

func (f dnsFixture) readyZone(t *testing.T, id string) dns.Zone {
	t.Helper()
	zone, err := f.service.CreateZone(context.Background(), "admin", "req-zone-"+id, "acc1",
		dns.CreateZoneInput{ID: id, ProviderZoneID: "pz-1"})
	if err != nil {
		t.Fatal(err)
	}
	return zone
}

func countRows(t *testing.T, db *storage.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestDNSAccountZoneRecordPersistAcrossRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fleet.db")
	f := newDNSFixtureAt(t, path, nil, newFakeProvider(), nil)
	account := f.readyAccount(t, "acc1")
	if !account.Enabled || account.ID != "acc1" {
		t.Fatalf("account = %+v", account)
	}
	zone, err := f.service.CreateZone(ctx, "admin", "req-zone", "acc1",
		dns.CreateZoneInput{ID: "z1", ProviderZoneID: "pz-1"})
	if err != nil || zone.Name != "example.com" || zone.Status != dns.ZoneActive {
		t.Fatalf("zone = %+v, %v", zone, err)
	}
	record, err := f.service.CreateRecord(ctx, "admin", "req-record", "z1", dns.RecordInput{
		Type: dns.RecordA, Name: "www", Content: "192.0.2.10", TTL: 300, Proxied: true,
	})
	if err != nil || record.ID != "rec-1" || !record.Proxied {
		t.Fatalf("record = %+v, %v", record, err)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newDNSFixtureAt(t, path, nil, newFakeProvider(), nil)
	accounts, err := reopened.service.ListAccounts(ctx, "admin")
	if err != nil || len(accounts) != 1 || accounts[0].ID != "acc1" {
		t.Fatalf("accounts after restart = %+v, %v", accounts, err)
	}
	zones, err := reopened.service.ListZones(ctx, "admin", "acc1")
	if err != nil || len(zones) != 1 || zones[0].ProviderZoneID != "pz-1" {
		t.Fatalf("zones after restart = %+v, %v", zones, err)
	}
	records, err := reopened.service.ListRecords(ctx, "admin", "z1")
	if err != nil || len(records) != 1 || records[0].Content != "192.0.2.10" {
		t.Fatalf("records after restart = %+v, %v", records, err)
	}
	page, err := reopened.audit.Query(ctx, audit.Query{Action: dns.ActionRecordCreate})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit after restart = %+v, %v", page, err)
	}
}

func TestDNSRecordCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newDNSFixture(t, nil, newFakeProvider(), nil)
	f.readyAccount(t, "acc1")
	f.readyZone(t, "z1")
	created, err := f.service.CreateRecord(ctx, "admin", "req-create", "z1", dns.RecordInput{
		Type: dns.RecordA, Name: "www", Content: "192.0.2.10", TTL: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.service.UpdateRecord(ctx, "admin", "req-update", created.ID, dns.RecordInput{
		Type: dns.RecordCNAME, Name: "www", Content: "origin.example.com", TTL: 600, Proxied: true,
	})
	if err != nil || updated.Content != "origin.example.com" || updated.TTL != 600 {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	if f.provider.updateCalls != 1 {
		t.Fatalf("update calls = %d", f.provider.updateCalls)
	}
	records, err := f.service.ListRecords(ctx, "admin", "z1")
	if err != nil || len(records) != 1 || records[0].Content != "origin.example.com" {
		t.Fatalf("records = %+v, %v", records, err)
	}
	if err := f.service.DeleteRecord(ctx, "admin", "req-delete", created.ID); err != nil {
		t.Fatal(err)
	}
	if f.provider.deleteCalls != 1 {
		t.Fatalf("delete calls = %d", f.provider.deleteCalls)
	}
	records, err = f.service.ListRecords(ctx, "admin", "z1")
	if err != nil || len(records) != 0 {
		t.Fatalf("records after delete = %+v, %v", records, err)
	}
}

func TestDNSDuplicateZoneAndRecordConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newDNSFixture(t, nil, newFakeProvider(), nil)
	f.readyAccount(t, "acc1")
	f.readyZone(t, "z1")
	if _, err := f.service.CreateZone(ctx, "admin", "req-zone-2", "acc1",
		dns.CreateZoneInput{ID: "z2", ProviderZoneID: "pz-1"}); !errors.Is(err, dns.ErrZoneNameTaken) {
		t.Fatalf("duplicate zone err = %v", err)
	}
	input := dns.RecordInput{Type: dns.RecordA, Name: "www", Content: "192.0.2.10", TTL: 300}
	if _, err := f.service.CreateRecord(ctx, "admin", "req-record-1", "z1", input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateRecord(ctx, "admin", "req-record-2", "z1", input); !errors.Is(err, dns.ErrRecordConflict) {
		t.Fatalf("duplicate record err = %v", err)
	}
	records, err := f.service.ListRecords(ctx, "admin", "z1")
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %+v, %v", records, err)
	}
}

func TestDNSSingleDefaultIsAtomic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newDNSFixture(t, nil, newFakeProvider(), nil)
	if _, err := f.service.CreateAccount(ctx, "admin", "req-1", dns.CreateAccountInput{
		ID: "a1", DisplayName: "One", MakeDefault: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateAccount(ctx, "admin", "req-2", dns.CreateAccountInput{
		ID: "a2", DisplayName: "Two", MakeDefault: true,
	}); err != nil {
		t.Fatal(err)
	}
	accounts, err := f.service.ListAccounts(ctx, "admin")
	if err != nil || defaultCount(accounts) != 1 {
		t.Fatalf("accounts = %+v, %v", accounts, err)
	}
	if _, err := f.service.SetDefault(ctx, "admin", "req-3", "a1"); err != nil {
		t.Fatal(err)
	}
	accounts, err = f.service.ListAccounts(ctx, "admin")
	if err != nil || defaultCount(accounts) != 1 || !accountByID(accounts, "a1").IsDefault {
		t.Fatalf("accounts after swap = %+v, %v", accounts, err)
	}
}

func TestDNSDefaultSwapRollsBackOnAuditFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newDNSFixture(t, nil, newFakeProvider(), actionsExcept(dns.ActionAccountDefault))
	if _, err := f.service.CreateAccount(ctx, "admin", "req-1", dns.CreateAccountInput{
		ID: "a1", DisplayName: "One", MakeDefault: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateAccount(ctx, "admin", "req-2", dns.CreateAccountInput{
		ID: "a2", DisplayName: "Two",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SetDefault(ctx, "admin", "req-3", "a2"); err == nil {
		t.Fatal("default swap succeeded without an audit record")
	}
	accounts, err := f.service.ListAccounts(ctx, "admin")
	if err != nil || !accountByID(accounts, "a1").IsDefault || accountByID(accounts, "a2").IsDefault {
		t.Fatalf("default state after rollback = %+v, %v", accounts, err)
	}
}

func TestDNSDeniedWriteIsAuditedWithoutMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newDNSFixture(t, authorizerFunc(func(context.Context, auth.Check) error {
		return auth.ErrDenied
	}), newFakeProvider(), nil)
	if _, err := f.service.CreateAccount(ctx, "admin", "req-deny", dns.CreateAccountInput{
		ID: "a1", DisplayName: "One",
	}); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if count := countRows(t, f.db, "dns_accounts"); count != 0 {
		t.Fatalf("denied write inserted %d accounts", count)
	}
	page, err := f.audit.Query(ctx, audit.Query{Action: dns.ActionAccountCreate, Result: "denied"})
	if err != nil || page.Total != 1 {
		t.Fatalf("denial audit = %+v, %v", page, err)
	}
}

func TestDNSAuditFailureRollsBackCreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newDNSFixture(t, nil, newFakeProvider(), []string{dns.ActionAccountUpdate})
	if _, err := f.service.CreateAccount(ctx, "admin", "req-1", dns.CreateAccountInput{
		ID: "a1", DisplayName: "One",
	}); err == nil {
		t.Fatal("account created without an audit record")
	}
	if count := countRows(t, f.db, "dns_accounts"); count != 0 {
		t.Fatalf("account survived audit failure: %d", count)
	}
}

func TestDNSNilProviderFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newDNSFixture(t, nil, nil, nil)
	account, err := f.service.CreateAccount(ctx, "admin", "req-1", dns.CreateAccountInput{
		ID: "a1", DisplayName: "One", Enabled: true, Credential: "ref-a1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.Enabled != true || account.CredentialStatus != dns.CredentialPendingVerification {
		t.Fatalf("account = %+v", account)
	}
	if _, err := f.service.TestAccount(ctx, "admin", "req-2", "a1"); !errors.Is(err, dns.ErrProviderUnavailable) {
		t.Fatalf("test err = %v", err)
	}
	if _, err := f.service.CreateZone(ctx, "admin", "req-3", "a1",
		dns.CreateZoneInput{ID: "z1", ProviderZoneID: "pz-1"}); !errors.Is(err, dns.ErrProviderUnavailable) {
		t.Fatalf("zone err = %v", err)
	}
	if _, err := f.service.UpdateAccount(ctx, "admin", "req-4", "a1", dns.UpdateAccountInput{
		DisplayName: "One Updated", Enabled: true,
	}); err != nil {
		t.Fatalf("local account update failed: %v", err)
	}
}

func TestDNSCredentialReferenceNeverExposed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const secretRef = "ref-super-secret-token-1"
	f := newDNSFixture(t, nil, newFakeProvider(), nil)
	view, err := f.service.CreateAccount(ctx, "admin", "req-1", dns.CreateAccountInput{
		ID: "a1", DisplayName: "One", Enabled: true, Credential: secretRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.TestAccount(ctx, "admin", "req-2", "a1"); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secretRef) || strings.Contains(string(encoded), "CredentialRef") {
		t.Fatalf("view leaked credential reference: %s", encoded)
	}
	accounts, err := f.service.ListAccounts(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(accounts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secretRef) {
		t.Fatalf("list leaked credential reference: %s", encoded)
	}
	page, err := f.audit.Query(ctx, audit.Query{})
	if err != nil || page.Total == 0 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
	encoded, err = json.Marshal(page.Items)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secretRef) {
		t.Fatalf("audit leaked credential reference: %s", encoded)
	}
	if _, err := dns.ParseCredentialRef("has whitespace"); !errors.Is(err, dns.ErrInvalidCredentialRef) {
		t.Fatalf("whitespace ref err = %v", err)
	}
	if _, err := dns.ParseCredentialRef(strings.Repeat("a", 257)); !errors.Is(err, dns.ErrInvalidCredentialRef) {
		t.Fatalf("oversized ref err = %v", err)
	}
	if _, err := f.service.CreateAccount(ctx, "admin", "req-3", dns.CreateAccountInput{
		ID: "a2", DisplayName: "Two", Credential: "bad ref",
	}); !errors.Is(err, dns.ErrInvalidCredentialRef) {
		t.Fatalf("create with raw-ish ref err = %v", err)
	}
}

func TestDNSRecordValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newDNSFixture(t, nil, newFakeProvider(), nil)
	f.readyAccount(t, "acc1")
	f.readyZone(t, "z1")
	priority := 10
	cases := []struct {
		name  string
		input dns.RecordInput
	}{
		{"mx without priority", dns.RecordInput{Type: dns.RecordMX, Name: "mail", Content: "mail.example.com", TTL: 300}},
		{"mx priority too large", dns.RecordInput{Type: dns.RecordMX, Name: "mail", Content: "mail.example.com", TTL: 300, Priority: intPtr(70000)}},
		{"priority on a", dns.RecordInput{Type: dns.RecordA, Name: "www", Content: "192.0.2.1", TTL: 300, Priority: &priority}},
		{"proxied txt", dns.RecordInput{Type: dns.RecordTXT, Name: "txt", Content: "hello", TTL: 300, Proxied: true}},
		{"ttl too large", dns.RecordInput{Type: dns.RecordA, Name: "www", Content: "192.0.2.1", TTL: 86401}},
		{"control character", dns.RecordInput{Type: dns.RecordTXT, Name: "txt", Content: "hello\nworld", TTL: 300}},
		{"unknown type", dns.RecordInput{Type: "SRV", Name: "svc", Content: "value", TTL: 300}},
		{"name whitespace", dns.RecordInput{Type: dns.RecordA, Name: "www example", Content: "192.0.2.1", TTL: 300}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := f.service.CreateRecord(ctx, "admin", "req-bad", "z1", testCase.input); !errors.Is(err, dns.ErrInvalidInput) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if f.provider.createCalls != 0 {
		t.Fatalf("provider called for invalid input: %d", f.provider.createCalls)
	}
	created, err := f.service.CreateRecord(ctx, "admin", "req-mx", "z1", dns.RecordInput{
		Type: dns.RecordMX, Name: "mail", Content: "mail.example.com", TTL: 300, Priority: intPtr(10),
	})
	if err != nil || created.Priority != 10 {
		t.Fatalf("mx record = %+v, %v", created, err)
	}
}

func TestDNSTestAccountRecordsStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	provider := newFakeProvider()
	provider.test = dns.ConnectionTest{OK: false, Status: dns.CredentialMissingPermissions}
	f := newDNSFixture(t, nil, provider, nil)
	if _, err := f.service.CreateAccount(ctx, "admin", "req-1", dns.CreateAccountInput{
		ID: "a1", DisplayName: "One", Enabled: true, Credential: "ref-a1",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.TestAccount(ctx, "admin", "req-2", "a1")
	if err != nil || result.Status != dns.CredentialMissingPermissions {
		t.Fatalf("test result = %+v, %v", result, err)
	}
	accounts, err := f.service.ListAccounts(ctx, "admin")
	if err != nil || accounts[0].CredentialStatus != dns.CredentialMissingPermissions {
		t.Fatalf("accounts = %+v, %v", accounts, err)
	}
	page, err := f.audit.Query(ctx, audit.Query{Action: dns.ActionAccountTest, Result: "failure"})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
	provider.testErr = errors.New("provider rejected ref-super-secret")
	provider.test = dns.ConnectionTest{}
	if _, err := f.service.TestAccount(ctx, "admin", "req-3", "a1"); !errors.Is(err, dns.ErrProviderFailed) {
		t.Fatalf("provider error = %v", err)
	} else if strings.Contains(err.Error(), "ref-super-secret") {
		t.Fatalf("provider error leaked secret: %v", err)
	}
}

func intPtr(value int) *int { return &value }

func defaultCount(accounts []dns.AccountView) int {
	count := 0
	for _, account := range accounts {
		if account.IsDefault {
			count++
		}
	}
	return count
}

func accountByID(accounts []dns.AccountView, id string) dns.AccountView {
	for _, account := range accounts {
		if account.ID == id {
			return account
		}
	}
	return dns.AccountView{}
}
