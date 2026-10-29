package notifications_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/notifications"
)

type allowNotificationsRead struct{}

func (allowNotificationsRead) AuthorizeNotificationsRead(ctx context.Context, subject string) error {
	return nil
}

func TestHTTPChannelCredentialAndTest(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	mux := http.NewServeMux()
	if err := notifications.RegisterHTTP(mux, f.service, func(*http.Request) (notifications.Principal, error) {
		return notifications.Principal{SubjectID: "admin-1", RequestID: "req-http"}, nil
	}, allowNotificationsRead{}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	list, err := http.Get(server.URL + "/api/v1/notifications/channels")
	if err != nil || list.StatusCode != http.StatusOK {
		t.Fatalf("list = %v %d", err, list.StatusCode)
	}
	list.Body.Close()
	body, _ := json.Marshal(map[string]any{
		"enabled": true,
		"smtp":    map[string]any{"host": "smtp.example.com", "port": 587, "security": "starttls", "sender": "a@example.com", "recipient": "b@example.com"},
	})
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/api/v1/notifications/channels/smtp", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	put, err := http.DefaultClient.Do(request)
	if err != nil || put.StatusCode != http.StatusOK {
		t.Fatalf("put = %v %d", err, put.StatusCode)
	}
	raw, _ := io.ReadAll(put.Body)
	put.Body.Close()
	if strings.Contains(string(raw), "secretRef") || strings.Contains(string(raw), "vault://") {
		t.Fatalf("channel view leaked a secret reference: %s", raw)
	}
	cred, _ := json.Marshal(map[string]string{"secretRef": "vault://smtp/primary"})
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/notifications/channels/smtp/credentials", bytes.NewReader(cred))
	request.Header.Set("Content-Type", "application/json")
	set, err := http.DefaultClient.Do(request)
	if err != nil || set.StatusCode != http.StatusOK {
		t.Fatalf("credential = %v %d", err, set.StatusCode)
	}
	set.Body.Close()
	test, err := http.Post(server.URL+"/api/v1/notifications/channels/smtp/test", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil || test.StatusCode != http.StatusOK {
		t.Fatalf("test = %v %d", err, test.StatusCode)
	}
	var delivery struct {
		Status notifications.DeliveryStatus `json:"status"`
	}
	json.NewDecoder(test.Body).Decode(&delivery)
	test.Body.Close()
	if delivery.Status != notifications.DeliveryProviderAccepted {
		t.Fatalf("delivery = %+v", delivery)
	}
	bad, _ := json.Marshal(map[string]any{"smtpFallbackOnQQFailure": true, "maxRetryAttempts": 9})
	request, _ = http.NewRequest(http.MethodPut, server.URL+"/api/v1/notifications/settings", bytes.NewReader(bad))
	request.Header.Set("Content-Type", "application/json")
	invalid, err := http.DefaultClient.Do(request)
	if err != nil || invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid settings = %v %d", err, invalid.StatusCode)
	}
	invalid.Body.Close()
}
