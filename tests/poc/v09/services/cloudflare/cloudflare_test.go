package cloudflare_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

const credentialRef = dns.CredentialRef("env:cloudflare-test")

type cfProvider struct {
	client *http.Client
	token  string
	base   string
}

func (p *cfProvider) do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, p.base+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+p.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("cloudflare: decode response: %w", err)
	}
	if !envelope.Success {
		codes := make([]string, 0, len(envelope.Errors))
		for _, item := range envelope.Errors {
			codes = append(codes, strconv.Itoa(item.Code))
		}
		return nil, fmt.Errorf("cloudflare: api failed status=%d codes=%s", response.StatusCode, strings.Join(codes, ","))
	}
	return envelope.Result, nil
}

func (p *cfProvider) Test(ctx context.Context, ref dns.CredentialRef) (dns.ConnectionTest, error) {
	if ref != credentialRef {
		return dns.ConnectionTest{}, fmt.Errorf("cloudflare: unknown credential ref")
	}
	type verify struct {
		Status string `json:"status"`
	}
	raw, err := p.do(ctx, http.MethodGet, "/user/tokens/verify", nil)
	if err != nil {
		return dns.ConnectionTest{}, err
	}
	var result verify
	if err := json.Unmarshal(raw, &result); err != nil || result.Status != "active" {
		return dns.ConnectionTest{OK: false, Status: dns.CredentialInvalid}, nil
	}
	if _, err := p.do(ctx, http.MethodGet, "/zones?per_page=1", nil); err != nil {
		return dns.ConnectionTest{OK: false, Status: dns.CredentialInvalid}, nil
	}
	return dns.ConnectionTest{OK: true, Status: dns.CredentialValid}, nil
}

func (p *cfProvider) ListZones(ctx context.Context, ref dns.CredentialRef) ([]dns.ZoneInfo, error) {
	if ref != credentialRef {
		return nil, fmt.Errorf("cloudflare: unknown credential ref")
	}
	raw, err := p.do(ctx, http.MethodGet, "/zones?per_page=50", nil)
	if err != nil {
		return nil, err
	}
	var zones []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &zones); err != nil {
		return nil, err
	}
	out := make([]dns.ZoneInfo, 0, len(zones))
	for _, zone := range zones {
		out = append(out, dns.ZoneInfo{ProviderZoneID: zone.ID, Name: zone.Name, Status: zone.Status})
	}
	return out, nil
}

type recordBody struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl,omitempty"`
	Proxied  bool   `json:"proxied"`
	Priority *int   `json:"priority,omitempty"`
}

func bodyFrom(input dns.RecordInput) recordBody {
	return recordBody{
		Type: string(input.Type), Name: input.Name, Content: input.Content,
		TTL: input.TTL, Proxied: input.Proxied, Priority: input.Priority,
	}
}

func (p *cfProvider) CreateRecord(ctx context.Context, ref dns.CredentialRef, zoneProviderID string, input dns.RecordInput) (string, error) {
	if ref != credentialRef {
		return "", fmt.Errorf("cloudflare: unknown credential ref")
	}
	raw, err := p.do(ctx, http.MethodPost, "/zones/"+zoneProviderID+"/dns_records", bodyFrom(input))
	if err != nil {
		return "", err
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.ID == "" {
		return "", fmt.Errorf("cloudflare: create returned no id")
	}
	return created.ID, nil
}

func (p *cfProvider) UpdateRecord(ctx context.Context, ref dns.CredentialRef, zoneProviderID, recordProviderID string, input dns.RecordInput) error {
	if ref != credentialRef {
		return fmt.Errorf("cloudflare: unknown credential ref")
	}
	_, err := p.do(ctx, http.MethodPatch, "/zones/"+zoneProviderID+"/dns_records/"+recordProviderID, bodyFrom(input))
	return err
}

func (p *cfProvider) DeleteRecord(ctx context.Context, ref dns.CredentialRef, zoneProviderID, recordProviderID string) error {
	if ref != credentialRef {
		return fmt.Errorf("cloudflare: unknown credential ref")
	}
	_, err := p.do(ctx, http.MethodDelete, "/zones/"+zoneProviderID+"/dns_records/"+recordProviderID, nil)
	return err
}

type allowAuthorizer struct{}

func (allowAuthorizer) Authorize(context.Context, auth.Check) error { return nil }

// TestCloudflareDNS is an explicitly authorized integration test. It requires
// CLOUDFLARE_API_TOKEN and only creates one random TXT record in xihuani.com,
// then deletes it. Skipped without the environment variable.
func TestCloudflareDNS(t *testing.T) {
	token := os.Getenv("CLOUDFLARE_API_TOKEN")
	if token == "" {
		t.Skip("CLOUDFLARE_API_TOKEN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db := testkit.NewDB(t, dns.Migrations(), audit.Migrations())
	repo, err := dns.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(dns.ServiceActions(), limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	provider := &cfProvider{client: &http.Client{Timeout: 30 * time.Second}, token: token,
		base: "https://api.cloudflare.com/client/v4"}
	service, err := dns.NewService(dns.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: allowAuthorizer{}, Provider: provider,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := service.CreateAccount(ctx, "admin", "req-cf-account", dns.CreateAccountInput{
		ID: "cf-xihuani", DisplayName: "Cloudflare xihuani integration",
		Enabled: true, MakeDefault: true, Credential: credentialRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	test, err := service.TestAccount(ctx, "admin", "req-cf-test", account.ID)
	if err != nil || !test.OK {
		t.Fatalf("credential test = %+v, %v", test, err)
	}
	providerZones, err := provider.ListZones(ctx, credentialRef)
	if err != nil {
		t.Fatal(err)
	}
	var providerZoneID string
	for _, zone := range providerZones {
		if zone.Name == "xihuani.com" {
			providerZoneID = zone.ProviderZoneID
		}
	}
	if providerZoneID == "" {
		t.Fatalf("xihuani.com not visible to token; zones=%d", len(providerZones))
	}
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	name := "sf-it-" + hex.EncodeToString(random)
	zone, err := service.CreateZone(ctx, "admin", "req-cf-zone", account.ID, dns.CreateZoneInput{
		ID: "zone-xihuani", ProviderZoneID: providerZoneID,
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := service.CreateRecord(ctx, "admin", "req-cf-record", zone.ID, dns.RecordInput{
		Type: dns.RecordTXT, Name: name, Content: "serverfleet dns integration test", TTL: 60,
	})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := service.DeleteRecord(cleanup, "admin", "req-cf-cleanup", record.ID); err != nil {
			t.Logf("cleanup record %s failed: %v", record.ID, err)
		}
	}()
	records, err := service.ListRecords(ctx, "admin", zone.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range records {
		if item.ID == record.ID && item.Name == name && item.Content == "serverfleet dns integration test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("created record %s not returned by local/provider read", record.ID)
	}
	t.Logf("cloudflare integration pass: zone=%s record=%s name=%s", zone.Name, record.ID, name)
}
