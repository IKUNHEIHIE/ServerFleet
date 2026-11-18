package auth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCredentialHTTPMetadataOnlyAndSourceFailure(t *testing.T) {
	vault, _, _, session := credentialFixture(t)
	h, err := NewIdentityHTTP(vault.identity, IdentityHTTPConfig{Origin: "https://fleet.test", Credentials: vault})
	if err != nil {
		t.Fatal(err)
	}
	input := `{"name":"Synthetic","kind":"ai_api_key","secret":"http-synthetic-secret","usageScope":["ai"]}`
	headers := map[string]string{"Idempotency-Key": "credential-http-create-01"}
	first := authRequest(t, h, "POST", "/api/v1/credentials", input, session.Secret, session.CSRF, headers)
	if first.Code != 201 || strings.Contains(first.Body.String(), "http-synthetic-secret") || first.Header().Get("Set-Cookie") != "" {
		t.Fatal(first.Code, first.Body.String())
	}
	replay := authRequest(t, h, "POST", "/api/v1/credentials", input, session.Secret, session.CSRF, headers)
	if replay.Code != 201 || first.Body.String() != replay.Body.String() {
		t.Fatal("idempotence", replay.Code)
	}
	listing := authRequest(t, h, "GET", "/api/v1/credentials", "", session.Secret, "", nil)
	var items []CredentialMetadata
	if listing.Code != 200 || json.Unmarshal(listing.Body.Bytes(), &items) != nil || len(items) != 1 {
		t.Fatal(listing.Code, listing.Body.String())
	}
	refs := authRequest(t, h, "GET", "/api/v1/credentials/"+items[0].ID+"/references", "", session.Secret, "", nil)
	if refs.Code != 503 {
		t.Fatal("missing source fabricated empty references", refs.Code)
	}
	noCSRF := authRequest(t, h, "POST", "/api/v1/credentials", input, session.Secret, "", headers)
	if noCSRF.Code != 403 {
		t.Fatal(noCSRF.Code)
	}
	unknown := authRequest(t, h, "POST", "/api/v1/credentials/"+items[0].ID+"/rotate", input, session.Secret, session.CSRF, headers)
	if unknown.Code == 202 {
		t.Fatal("accepted without task submission")
	}
}

func TestRemoveAIKeyThroughSettings(t *testing.T) {
	vault, _, _, session := credentialFixture(t)
	h, err := NewIdentityHTTP(vault.identity, IdentityHTTPConfig{Origin: "https://fleet.test", Credentials: vault})
	if err != nil {
		t.Fatal(err)
	}
	key, err := vault.Create(context.Background(), session.Secret, session.CSRF, CredentialInput{
		Name: "Retired AI key", Kind: "ai_api_key", Secret: "synthetic-removal-test", UsageScope: []string{"ai"},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/credentials/" + key.ID
	remove := func(revision, csrf string) int {
		return authRequest(t, h, "DELETE", path, "{\"expectedRevision\":"+revision+"}", session.Secret, csrf, nil).Code
	}
	if got := remove("1", ""); got != 403 {
		t.Fatalf("missing CSRF: %d", got)
	}
	if got := remove("2", session.CSRF); got != 412 {
		t.Fatalf("stale revision: %d", got)
	}
	if got := remove("1", session.CSRF); got != 204 {
		t.Fatalf("delete: %d", got)
	}
	if got := remove("1", session.CSRF); got != 204 {
		t.Fatalf("retry of committed deletion: %d", got)
	}
	items, err := vault.List(context.Background(), session.Secret)
	if err != nil || len(items) != 1 || items[0].Status != "revoked" || items[0].Revision != key.Revision+1 {
		t.Fatalf("deleted key metadata: %v, %v", items, err)
	}
	err = vault.identity.read(context.Background(), func(st *identityState, _ time.Time) error {
		if len(st.Credentials[key.ID].Sealed.Ciphertext) != 0 {
			t.Error("deleted AI key still has ciphertext")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
