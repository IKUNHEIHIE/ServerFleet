package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"serverfleet.local/serverfleet/internal/auth"
	"strconv"
	"time"
)

type Principal struct {
	SubjectID string
	RequestID string
}

type Authenticator func(*http.Request) (Principal, error)

type ReadGate interface {
	AuthorizeNotificationsRead(ctx context.Context, subjectID string) error
}

type httpAPI struct {
	service       *Service
	authenticator Authenticator
	reads         ReadGate
}

func RegisterHTTP(mux *http.ServeMux, service *Service, authenticator Authenticator, reads ReadGate) error {
	if mux == nil || service == nil || authenticator == nil {
		return ErrServiceNotConfigured
	}
	api := &httpAPI{service: service, authenticator: authenticator, reads: reads}
	mux.HandleFunc("GET /api/v1/notifications/channels", api.listChannels)
	mux.HandleFunc("GET /api/v1/notifications/channels/{channel}", api.getChannel)
	mux.HandleFunc("PUT /api/v1/notifications/channels/{channel}", api.putChannel)
	mux.HandleFunc("POST /api/v1/notifications/channels/{channel}/credentials", api.setCredential)
	mux.HandleFunc("POST /api/v1/notifications/channels/{channel}/test", api.testChannel)
	mux.HandleFunc("GET /api/v1/notifications/settings", api.getSettings)
	mux.HandleFunc("PUT /api/v1/notifications/settings", api.putSettings)
	mux.HandleFunc("GET /api/v1/notifications/deliveries", api.listDeliveries)
	mux.HandleFunc("POST /api/v1/notifications/deliveries/{deliveryId}/retry", api.retryDelivery)
	return nil
}

type channelDTO struct {
	CredentialID       string           `json:"credentialId,omitempty"`
	AutomationOwnerID  string           `json:"automationOwnerId,omitempty"`
	CredentialRevision uint64           `json:"credentialRevision,omitempty"`
	BackgroundReady    bool             `json:"backgroundReady,omitempty"`
	Channel            Channel          `json:"channel"`
	Enabled            bool             `json:"enabled"`
	CredentialStatus   CredentialStatus `json:"credentialStatus"`
	SMTP               *SMTPSettings    `json:"smtp,omitempty"`
	QQBot              *QQSettings      `json:"qqbot,omitempty"`
	UpdatedAt          string           `json:"updatedAt,omitempty"`
}

type deliveryDTO struct {
	TaskID       string         `json:"taskId,omitempty"`
	ID           string         `json:"id"`
	Channel      Channel        `json:"channel"`
	Status       DeliveryStatus `json:"status"`
	ProviderCode string         `json:"providerCode,omitempty"`
	Message      string         `json:"message"`
	CreatedAt    string         `json:"createdAt"`
}

func (api *httpAPI) listChannels(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if !api.authorizeRead(w, r, principal) {
		return
	}
	records, err := api.service.ListChannels(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]channelDTO, 0, len(records))
	for _, record := range records {
		items = append(items, dtoFromChannel(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api *httpAPI) getChannel(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if !api.authorizeRead(w, r, principal) {
		return
	}
	channel := Channel(r.PathValue("channel"))
	if !channel.Valid() {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "channel is invalid")
		return
	}
	record, err := api.service.GetChannel(r.Context(), channel)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromChannel(record))
}

func (api *httpAPI) putChannel(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	channel := Channel(r.PathValue("channel"))
	if !channel.Valid() {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "channel is invalid")
		return
	}
	var body ChannelInput
	if !decodeJSON(w, r, &body) {
		return
	}
	record, err := api.service.UpsertChannel(r.Context(), principal.SubjectID, principal.RequestID, channel, body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromChannel(record))
}

func (api *httpAPI) setCredential(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	channel := Channel(r.PathValue("channel"))
	if !channel.Valid() {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "channel is invalid")
		return
	}
	var body struct {
		SecretRef string `json:"secretRef"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	record, err := api.service.SetSecretRef(r.Context(), principal.SubjectID, principal.RequestID, channel, body.SecretRef)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromChannel(record))
}

func (api *httpAPI) testChannel(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	channel := Channel(r.PathValue("channel"))
	if !channel.Valid() {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "channel is invalid")
		return
	}
	var body struct {
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	delivery, err := api.service.TestChannel(r.Context(), principal.SubjectID, principal.RequestID, channel, body.IdempotencyKey)
	if err != nil && delivery.ID == 0 {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromDelivery(delivery))
}

func (api *httpAPI) getSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if !api.authorizeRead(w, r, principal) {
		return
	}
	settings, err := api.service.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (api *httpAPI) putSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body Settings
	if !decodeJSON(w, r, &body) {
		return
	}
	settings, err := api.service.UpdateSettings(r.Context(), principal.SubjectID, principal.RequestID, body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (api *httpAPI) listDeliveries(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	if !api.authorizeRead(w, r, principal) {
		return
	}
	filter := DeliveryFilter{
		Channel: Channel(r.URL.Query().Get("channel")),
		Status:  DeliveryStatus(r.URL.Query().Get("status")),
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > MaxDeliveryLimit {
			writeFixed(w, http.StatusBadRequest, "invalid_request", "limit is invalid")
			return
		}
		filter.Limit = limit
	}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			writeFixed(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		filter.Offset = offset
	}
	deliveries, err := api.service.ListDeliveries(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]deliveryDTO, 0, len(deliveries))
	for _, delivery := range deliveries {
		items = append(items, dtoFromDelivery(delivery))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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

func (api *httpAPI) authorizeRead(w http.ResponseWriter, r *http.Request, principal Principal) bool {
	if api.reads == nil || api.reads.AuthorizeNotificationsRead(r.Context(), principal.SubjectID) != nil {
		writeFixed(w, http.StatusForbidden, "forbidden", "request is not permitted")
		return false
	}
	return true
}

func dtoFromChannel(record ChannelRecord) channelDTO {
	return channelDTO{
		CredentialID: record.SecretRef, AutomationOwnerID: record.AutomationOwnerID, CredentialRevision: record.CredentialRevision, BackgroundReady: record.BackgroundReady,
		Channel: record.Channel, Enabled: record.Enabled, CredentialStatus: record.CredentialStatus,
		SMTP: record.SMTP, QQBot: record.QQ, UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func dtoFromDelivery(delivery Delivery) deliveryDTO {
	return deliveryDTO{
		TaskID: delivery.TaskID, ID: strconv.FormatInt(delivery.ID, 10), Channel: delivery.Channel, Status: delivery.Status,
		ProviderCode: delivery.ProviderCode, Message: delivery.Message,
		CreatedAt: delivery.CreatedAt.UTC().Format(time.RFC3339),
	}
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
	case errors.Is(err, ErrChannelNotFound), errors.Is(err, ErrDeliveryNotFound):
		writeFixed(w, http.StatusNotFound, "not_found", "resource was not found")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrChannelNotConfigured), errors.Is(err, ErrProviderUnavailable), errors.Is(err, ErrSecretUnavailable):
		writeFixed(w, http.StatusServiceUnavailable, "notifications_unavailable", "notification channel is not configured")
	case errors.Is(err, ErrProviderRejected), errors.Is(err, ErrProviderRateLimited):
		writeFixed(w, http.StatusBadGateway, "provider_rejected", "notification provider rejected the message")
	case errors.Is(err, ErrDeliveryUnknown):
		writeFixed(w, http.StatusBadGateway, "delivery_unknown", "notification outcome is unknown")
	case errors.Is(err, ErrRetryLimit), errors.Is(err, ErrRetryBackoff), errors.Is(err, ErrNotRetryable):
		writeFixed(w, http.StatusConflict, "conflict", "delivery cannot be retried")
	case errors.Is(err, ErrInvalidChannel), errors.Is(err, ErrInvalidConfig), errors.Is(err, ErrInvalidSettings),
		errors.Is(err, ErrInvalidSecretRef), errors.Is(err, ErrInvalidMessage), errors.Is(err, ErrInvalidDedupeKey),
		errors.Is(err, ErrInvalidQuery), errors.Is(err, ErrRequestIDRequired):
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}

func (api *httpAPI) retryDelivery(w http.ResponseWriter, r *http.Request) {
	p, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct{}
	if r.URL.RawQuery != "" {
		writeError(w, ErrInvalidQuery)
		return
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	id, e := strconv.ParseInt(r.PathValue("deliveryId"), 10, 64)
	if e != nil || id <= 0 {
		writeError(w, ErrInvalidQuery)
		return
	}
	d, e := api.service.RetryDelivery(r.Context(), p.SubjectID, p.RequestID, id)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, http.StatusAccepted, dtoFromDelivery(d))
}
