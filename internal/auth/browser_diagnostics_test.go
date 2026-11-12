package auth

import (
	"net/http/httptest"
	"testing"
)

func TestBrowserOriginAuthorityKeepsExplicitPortAndFixedDiagnostics(t *testing.T) {
	guard, e := NewBrowserGuard("https://fleet.test:8443")
	if e != nil {
		t.Fatal(e)
	}
	request := httptest.NewRequest("POST", "https://fleet.test:8443/api/v1/auth/login", nil)
	request.Header.Set("Origin", "https://fleet.test:8443")
	if e = guard.Check(request, true); e != nil {
		t.Fatal("matching explicit port rejected", e)
	}
	request.Host = "fleet.test"
	if code := guard.RejectionCode(request, true); code != "HOST_MISMATCH" {
		t.Fatal(code)
	}
	request.Host = "fleet.test:8443"
	request.Header.Set("Origin", "https://fleet.test")
	if code := guard.RejectionCode(request, true); code != "ORIGIN_MISMATCH" {
		t.Fatal(code)
	}
	request.Header.Del("Origin")
	if code := guard.RejectionCode(request, true); code != "ORIGIN_REQUIRED" {
		t.Fatal(code)
	}
}
