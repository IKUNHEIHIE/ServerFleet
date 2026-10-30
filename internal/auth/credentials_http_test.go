package auth

import (
	"encoding/json"
	"strings"
	"testing"
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
