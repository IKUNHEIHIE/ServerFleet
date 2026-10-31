package dns

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

type Principal struct {
	SubjectID string
	RequestID string
}

type Authenticator func(*http.Request) (Principal, error)

type httpAPI struct {
	service       *Service
	authenticator Authenticator
}

func RegisterHTTP(mux *http.ServeMux, service *Service, authenticator Authenticator) error {
	if mux == nil || service == nil || authenticator == nil {
		return ErrInvalidInput
	}
	api := &httpAPI{service: service, authenticator: authenticator}
	mux.HandleFunc("GET /api/v1/dns/providers", api.providers)
	mux.HandleFunc("GET /api/v1/dns/accounts", api.listAccounts)
	mux.HandleFunc("POST /api/v1/dns/accounts", api.createAccount)
	mux.HandleFunc("GET /api/v1/dns/accounts/{accountId}", api.getAccount)
	mux.HandleFunc("PATCH /api/v1/dns/accounts/{accountId}", api.updateAccount)
	mux.HandleFunc("POST /api/v1/dns/accounts/{accountId}/default", api.setDefault)
	mux.HandleFunc("POST /api/v1/dns/accounts/{accountId}/test", api.testAccount)
	mux.HandleFunc("GET /api/v1/dns/accounts/{accountId}/zones", api.listZones)
	mux.HandleFunc("POST /api/v1/dns/accounts/{accountId}/zones/sync", api.syncZone)
	mux.HandleFunc("GET /api/v1/dns/zones/{zoneId}/records", api.listRecords)
	mux.HandleFunc("POST /api/v1/dns/zones/{zoneId}/records", api.createRecord)
	mux.HandleFunc("PATCH /api/v1/dns/zones/{zoneId}/records/{recordId}", api.updateRecord)
	mux.HandleFunc("DELETE /api/v1/dns/zones/{zoneId}/records/{recordId}", api.deleteRecord)
	return nil
}

type accountDTO struct {
	ID               string `json:"id"`
	ProviderCode     string `json:"providerCode"`
	DisplayName      string `json:"displayName"`
	Note             string `json:"note,omitempty"`
	Enabled          bool   `json:"enabled"`
	IsDefault        bool   `json:"isDefault"`
	CredentialStatus string `json:"credentialStatus"`
	CreatedAt        string `json:"createdAt,omitempty"`
}

type zoneDTO struct {
	ID        string `json:"id"`
	AccountID string `json:"accountId"`
	Name      string `json:"name"`
	Status    string `json:"status,omitempty"`
}

type recordDTO struct {
	ID         string `json:"id"`
	ZoneID     string `json:"zoneId"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	TTL        int    `json:"ttl,omitempty"`
	Proxied    bool   `json:"proxied"`
	Priority   int    `json:"priority,omitempty"`
	ModifiedAt string `json:"modifiedAt,omitempty"`
}

func (api *httpAPI) providers(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.principal(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": []map[string]string{{"code": "cloudflare", "name": "Cloudflare"}}})
}

func (api *httpAPI) listAccounts(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	accounts, err := api.service.ListAccounts(r.Context(), principal.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]accountDTO, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, dtoFromAccount(account))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api *httpAPI) createAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		DisplayName   string `json:"displayName"`
		Note          string `json:"note"`
		Enabled       *bool  `json:"enabled"`
		CredentialRef string `json:"credentialRef"`
		MakeDefault   bool   `json:"makeDefault"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	credential, err := ParseCredentialRef(body.CredentialRef)
	if body.CredentialRef == "" {
		credential, err = "", nil
	}
	if err != nil {
		writeError(w, err)
		return
	}
	account, err := api.service.CreateAccount(r.Context(), principal.SubjectID, principal.RequestID, CreateAccountInput{
		ID: newID("dnsacct"), DisplayName: body.DisplayName, Note: body.Note,
		Enabled: enabled, MakeDefault: body.MakeDefault, Credential: credential,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dtoFromAccount(account))
}

func (api *httpAPI) getAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	account, err := api.findAccount(r.Context(), principal.SubjectID, r.PathValue("accountId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromAccount(account))
}

func (api *httpAPI) updateAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	current, err := api.findAccount(r.Context(), principal.SubjectID, r.PathValue("accountId"))
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		DisplayName   *string `json:"displayName"`
		Note          *string `json:"note"`
		Enabled       *bool   `json:"enabled"`
		CredentialRef *string `json:"credentialRef"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	input := UpdateAccountInput{DisplayName: current.DisplayName, Note: current.Note, Enabled: current.Enabled}
	if body.DisplayName != nil {
		input.DisplayName = *body.DisplayName
	}
	if body.Note != nil {
		input.Note = *body.Note
	}
	if body.Enabled != nil {
		input.Enabled = *body.Enabled
	}
	if body.CredentialRef != nil {
		ref, err := ParseCredentialRef(*body.CredentialRef)
		if err != nil {
			writeError(w, err)
			return
		}
		input.Credential = ref
	}
	account, err := api.service.UpdateAccount(r.Context(), principal.SubjectID, principal.RequestID, current.ID, input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromAccount(account))
}

func (api *httpAPI) setDefault(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	account, err := api.service.SetDefault(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("accountId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromAccount(account))
}

func (api *httpAPI) testAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	test, err := api.service.TestAccount(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("accountId"))
	if err != nil {
		writeError(w, err)
		return
	}
	status := "failed"
	if test.OK {
		status = "success"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": status, "zoneRead": test.OK, "recordWrite": test.OK,
		"checkedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

func (api *httpAPI) listZones(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	zones, err := api.service.ListZones(r.Context(), principal.SubjectID, r.PathValue("accountId"))
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]zoneDTO, 0, len(zones))
	for _, zone := range zones {
		items = append(items, zoneDTO{ID: zone.ID, AccountID: zone.AccountID, Name: zone.Name, Status: string(zone.Status)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api *httpAPI) syncZone(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		ProviderZoneID string `json:"providerZoneId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	zone, err := api.service.CreateZone(r.Context(), principal.SubjectID, principal.RequestID,
		r.PathValue("accountId"), CreateZoneInput{ID: newID("dnszone"), ProviderZoneID: body.ProviderZoneID})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, zoneDTO{ID: zone.ID, AccountID: zone.AccountID, Name: zone.Name, Status: string(zone.Status)})
}

func (api *httpAPI) listRecords(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	records, err := api.service.ListRecords(r.Context(), principal.SubjectID, r.PathValue("zoneId"))
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]recordDTO, 0, len(records))
	for _, record := range records {
		items = append(items, dtoFromRecord(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api *httpAPI) createRecord(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	input, ok := api.recordInput(w, r)
	if !ok {
		return
	}
	record, err := api.service.CreateRecord(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("zoneId"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dtoFromRecord(record))
}

func (api *httpAPI) updateRecord(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	records, err := api.service.ListRecords(r.Context(), principal.SubjectID, r.PathValue("zoneId"))
	if err != nil {
		writeError(w, err)
		return
	}
	recordID := r.PathValue("recordId")
	found := false
	for _, record := range records {
		if record.ID == recordID {
			found = true
			break
		}
	}
	if !found {
		writeFixed(w, http.StatusNotFound, "not_found", "record was not found")
		return
	}
	input, ok := api.recordInput(w, r)
	if !ok {
		return
	}
	record, err := api.service.UpdateRecord(r.Context(), principal.SubjectID, principal.RequestID, recordID, input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromRecord(record))
}

func (api *httpAPI) deleteRecord(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if err := api.service.DeleteRecord(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("recordId")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *httpAPI) recordInput(w http.ResponseWriter, r *http.Request) (RecordInput, bool) {
	var body struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Content  string `json:"content"`
		TTL      int    `json:"ttl"`
		Proxied  bool   `json:"proxied"`
		Priority *int   `json:"priority"`
	}
	if !decodeJSON(w, r, &body) {
		return RecordInput{}, false
	}
	return RecordInput{
		Type: RecordType(body.Type), Name: body.Name, Content: body.Content,
		TTL: body.TTL, Proxied: body.Proxied, Priority: body.Priority,
	}, true
}

func (api *httpAPI) findAccount(ctx context.Context, subjectID, id string) (AccountView, error) {
	accounts, err := api.service.ListAccounts(ctx, subjectID)
	if err != nil {
		return AccountView{}, err
	}
	for _, account := range accounts {
		if account.ID == id {
			return account, nil
		}
	}
	return AccountView{}, ErrAccountNotFound
}

func (api *httpAPI) principal(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	principal, err := api.authenticator(r)
	if err != nil || principal.SubjectID == "" {
		writeFixed(w, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return Principal{}, false
	}
	if principal.RequestID == "" {
		writeFixed(w, http.StatusBadRequest, "missing_request_id", "request id is required")
		return Principal{}, false
	}
	return principal, true
}

func dtoFromAccount(account AccountView) accountDTO {
	return accountDTO{
		ID: account.ID, ProviderCode: account.ProviderCode, DisplayName: account.DisplayName,
		Note: account.Note, Enabled: account.Enabled, IsDefault: account.IsDefault,
		CredentialStatus: string(account.CredentialStatus), CreatedAt: formatTime(account.CreatedAt),
	}
}

func dtoFromRecord(record Record) recordDTO {
	return recordDTO{
		ID: record.ID, ZoneID: record.ZoneID, Type: string(record.Type), Name: record.Name,
		Content: record.Content, TTL: record.TTL, Proxied: record.Proxied,
		Priority: record.Priority, ModifiedAt: formatTime(record.ModifiedAt),
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeFixed(w, http.StatusBadRequest, "invalid_json", "request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeFixed(w, http.StatusBadRequest, "invalid_json", "request body must contain one object")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeFixed(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrDenied), errors.Is(err, auth.ErrInvalidCheck), errors.Is(err, auth.ErrPolicyUnavailable):
		writeFixed(w, http.StatusForbidden, "forbidden", "request is not permitted")
	case errors.Is(err, ErrAccountNotFound), errors.Is(err, ErrZoneNotFound), errors.Is(err, ErrRecordNotFound):
		writeFixed(w, http.StatusNotFound, "not_found", "resource was not found")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrAccountExists), errors.Is(err, ErrZoneExists), errors.Is(err, ErrZoneNameTaken),
		errors.Is(err, ErrRecordConflict):
		writeFixed(w, http.StatusConflict, "conflict", "resource already exists")
	case errors.Is(err, ErrProviderUnavailable), errors.Is(err, ErrCredentialNotConfigured),
		errors.Is(err, ErrCredentialUnverified):
		writeFixed(w, http.StatusServiceUnavailable, "dns_unavailable", "DNS provider is not available")
	case errors.Is(err, ErrProviderFailed), errors.Is(err, ErrZoneNotInProvider), errors.Is(err, ErrInvalidProviderResponse):
		writeFixed(w, http.StatusBadGateway, "provider_failed", "DNS provider request failed")
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidCredentialRef), errors.Is(err, ErrRequestIDRequired):
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}

func newID(prefix string) string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		panic("dns: entropy source failed")
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}
