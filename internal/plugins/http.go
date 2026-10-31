package plugins

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// LifecyclePrincipal is trusted Web composition data for plugin mutations. The
// lifecycle gate reads it from the SAME request context.
type LifecyclePrincipal struct {
	SubjectID     string
	RequestID     string
	Administrator bool
}

type lifecyclePrincipalKey struct{}

func WithLifecyclePrincipal(ctx context.Context, principal LifecyclePrincipal) context.Context {
	return context.WithValue(ctx, lifecyclePrincipalKey{}, principal)
}

func LifecyclePrincipalFromContext(ctx context.Context) (LifecyclePrincipal, bool) {
	if ctx == nil {
		return LifecyclePrincipal{}, false
	}
	principal, ok := ctx.Value(lifecyclePrincipalKey{}).(LifecyclePrincipal)
	return principal, ok
}

type PluginAuthenticator func(*http.Request) (LifecyclePrincipal, error)

// ManagementHTTP exposes plugin repository/catalog/installation management. It
// never downloads, installs or executes an unverified package.
type ManagementHTTP struct {
	lifecycle    *LifecycleManager
	repository   RepositoryStore
	operations   OperationStore
	authenticate PluginAuthenticator
	mux          *http.ServeMux
	now          func() time.Time
}

func NewManagementHTTP(lifecycle *LifecycleManager, repository RepositoryStore, operations OperationStore, authenticate PluginAuthenticator) (*ManagementHTTP, error) {
	if lifecycle == nil || repository == nil || operations == nil || authenticate == nil {
		return nil, ErrPackageRejected
	}
	h := &ManagementHTTP{lifecycle: lifecycle, repository: repository, operations: operations, authenticate: authenticate, mux: http.NewServeMux(), now: time.Now}
	h.mux.HandleFunc("GET /api/v1/plugins", h.list)
	h.mux.HandleFunc("GET /api/v1/plugins/repository", h.getRepository)
	h.mux.HandleFunc("PUT /api/v1/plugins/repository", h.putRepository)
	h.mux.HandleFunc("GET /api/v1/plugins/catalog", h.catalog)
	h.mux.HandleFunc("POST /api/v1/plugins/catalog/refresh", h.refreshCatalog)
	h.mux.HandleFunc("POST /api/v1/plugins/{pluginId}/install", h.install)
	h.mux.HandleFunc("PATCH /api/v1/plugins/{pluginId}/activation", h.activation)
	h.mux.HandleFunc("PUT /api/v1/plugins/{pluginId}/activation", h.activation)
	h.mux.HandleFunc("POST /api/v1/plugins/{pluginId}/update", h.update)
	h.mux.HandleFunc("DELETE /api/v1/plugins/{pluginId}", h.uninstall)
	h.mux.HandleFunc("GET /api/v1/plugins/{pluginId}", h.get)
	h.mux.HandleFunc("GET /api/v1/plugin-operations/{operationId}", h.operation)
	return h, nil
}

func (h *ManagementHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.TLS == nil {
		writePluginProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return
	}
	principal, err := h.authenticate(r)
	if err != nil || !principal.Administrator || !lifecycleID(principal.SubjectID) {
		writePluginProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return
	}
	ctx := WithLifecyclePrincipal(r.Context(), principal)
	h.mux.ServeHTTP(w, r.WithContext(ctx))
}

func writePluginProblem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code})
}

func writePluginJSON(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 1<<20 {
		writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_SOURCE_UNAVAILABLE")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func decodePluginBody(w http.ResponseWriter, r *http.Request, value any) bool {
	if len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Encoding") != "" || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	if _, err := decoder.Token(); err != io.EOF {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	return true
}

type installedPluginDTO struct {
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	Status       string   `json:"status"`
	Enabled      bool     `json:"enabled"`
	SourceURL    string   `json:"sourceURL,omitempty"`
	SourceCommit string   `json:"sourceCommit"`
	Permissions  []string `json:"permissions,omitempty"`
	LastError    string   `json:"lastError,omitempty"`
	InstalledAt  string   `json:"installedAt,omitempty"`
}

func installationStatus(phase string) string {
	switch phase {
	case "enabled":
		return "enabled"
	case "starting":
		return "starting"
	case "stopping":
		return "draining"
	case "installed_disabled", "disabled":
		return "installed_disabled"
	case "runtime_unknown", "stop_unknown":
		return "failed"
	default:
		return "installed_disabled"
	}
}

func installationDTO(installation Installation) installedPluginDTO {
	return installedPluginDTO{
		ID:           installation.ID,
		Version:      installation.Version,
		Status:       installationStatus(installation.Phase),
		Enabled:      installation.Enabled,
		SourceURL:    installation.Source,
		SourceCommit: "",
		Permissions:  []string{},
	}
}

func (h *ManagementHTTP) list(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	installation, err := h.lifecycle.Get(r.Context())
	if err != nil {
		writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_SOURCE_UNAVAILABLE")
		return
	}
	items := []installedPluginDTO{}
	if installation.Installed && installation.Phase != "uninstalled" {
		items = append(items, installationDTO(installation))
	}
	writePluginJSON(w, http.StatusOK, map[string]any{"items": items})
}

func pluginID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("pluginId")
	if id != "xray-manager" {
		writePluginProblem(w, http.StatusNotFound, "PLUGIN_NOT_FOUND")
		return "", false
	}
	return id, true
}

func (h *ManagementHTTP) get(w http.ResponseWriter, r *http.Request) {
	if _, ok := pluginID(w, r); !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	installation, err := h.lifecycle.Get(r.Context())
	if err != nil {
		writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_SOURCE_UNAVAILABLE")
		return
	}
	if !installation.Installed || installation.Phase == "uninstalled" {
		writePluginProblem(w, http.StatusNotFound, "PLUGIN_NOT_FOUND")
		return
	}
	writePluginJSON(w, http.StatusOK, installationDTO(installation))
}

func (h *ManagementHTTP) getRepository(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	config, err := h.repository.Get(r.Context())
	if err != nil {
		writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_REPOSITORY_UNAVAILABLE")
		return
	}
	writePluginJSON(w, http.StatusOK, config)
}

func (h *ManagementHTTP) putRepository(w http.ResponseWriter, r *http.Request) {
	var body RepositoryConfig
	if !decodePluginBody(w, r, &body) {
		return
	}
	if err := h.repository.Put(r.Context(), body); err != nil {
		writePluginProblem(w, http.StatusBadRequest, "PLUGIN_REPOSITORY_REJECTED")
		return
	}
	config, err := h.repository.Get(r.Context())
	if err != nil {
		writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_REPOSITORY_UNAVAILABLE")
		return
	}
	writePluginJSON(w, http.StatusOK, config)
}

func (h *ManagementHTTP) catalog(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	// No repository fetch adapter is configured in this release. An empty
	// catalog is honest; refresh reports the missing source explicitly.
	writePluginJSON(w, http.StatusOK, map[string]any{"items": []any{}})
}

func (h *ManagementHTTP) refreshCatalog(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	writePluginProblem(w, http.StatusNotImplemented, "PLUGIN_CATALOG_SOURCE_UNAVAILABLE")
}

type packageRequest struct {
	Version             string   `json:"version"`
	SourceCommit        string   `json:"sourceCommit"`
	ExpectedSHA256      string   `json:"expectedSha256"`
	AcceptedPermissions []string `json:"acceptedPermissions"`
	EnableAfterInstall  bool     `json:"enableAfterInstall"`
	DeferIfBusy         bool     `json:"deferIfBusy"`
}

func (h *ManagementHTTP) install(w http.ResponseWriter, r *http.Request) {
	if _, ok := pluginID(w, r); !ok {
		return
	}
	var body packageRequest
	if !decodePluginBody(w, r, &body) {
		return
	}
	if body.Version == "" || body.SourceCommit == "" || len(body.ExpectedSHA256) != 64 {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	writePluginProblem(w, http.StatusNotImplemented, "PLUGIN_PACKAGE_SOURCE_UNAVAILABLE")
}

func (h *ManagementHTTP) update(w http.ResponseWriter, r *http.Request) {
	if _, ok := pluginID(w, r); !ok {
		return
	}
	var body packageRequest
	if !decodePluginBody(w, r, &body) {
		return
	}
	writePluginProblem(w, http.StatusNotImplemented, "PLUGIN_PACKAGE_SOURCE_UNAVAILABLE")
}

func randomOperationID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", ErrOperationRejected
	}
	return hex.EncodeToString(raw[:]), nil
}

type pluginOperationDTO struct {
	ID              string         `json:"id"`
	PluginID        string         `json:"pluginId"`
	Action          string         `json:"action"`
	Status          string         `json:"status"`
	PreviousVersion string         `json:"previousVersion,omitempty"`
	TargetVersion   string         `json:"targetVersion,omitempty"`
	Error           *pluginOpError `json:"error,omitempty"`
	AuditID         string         `json:"auditId,omitempty"`
}
type pluginOpError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func operationDTO(record OperationRecord) pluginOperationDTO {
	dto := pluginOperationDTO{ID: record.ID, PluginID: record.PluginID, Action: record.Action, Status: record.Status, PreviousVersion: record.PreviousVersion, TargetVersion: record.TargetVersion, AuditID: record.AuditID}
	if record.ErrorCode != "" || record.ErrorMessage != "" {
		dto.Error = &pluginOpError{Code: record.ErrorCode, Message: record.ErrorMessage}
	}
	return dto
}

func (h *ManagementHTTP) activation(w http.ResponseWriter, r *http.Request) {
	if _, ok := pluginID(w, r); !ok {
		return
	}
	var body struct {
		Enabled             bool `json:"enabled"`
		DrainTimeoutSeconds int  `json:"drainTimeoutSeconds"`
	}
	if !decodePluginBody(w, r, &body) {
		return
	}
	if body.DrainTimeoutSeconds < 0 || body.DrainTimeoutSeconds > 3600 {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	installation, err := h.lifecycle.Get(r.Context())
	if err != nil {
		writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_SOURCE_UNAVAILABLE")
		return
	}
	if !installation.Installed || installation.Phase == "uninstalled" {
		writePluginProblem(w, http.StatusConflict, "PLUGIN_NOT_INSTALLED")
		return
	}
	if body.Enabled == installation.Enabled {
		record, err := h.recordNoopActivation(r.Context(), installation, body.Enabled)
		if err != nil {
			writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_OPERATION_UNAVAILABLE")
			return
		}
		writePluginJSON(w, http.StatusOK, operationDTO(record))
		return
	}
	if !h.lifecycle.HasRuntime() {
		writePluginProblem(w, http.StatusNotImplemented, "PLUGIN_RUNTIME_UNAVAILABLE")
		return
	}
	writePluginProblem(w, http.StatusNotImplemented, "PLUGIN_RUNTIME_UNAVAILABLE")
}

func (h *ManagementHTTP) recordNoopActivation(ctx context.Context, installation Installation, enabled bool) (OperationRecord, error) {
	id, err := randomOperationID()
	if err != nil {
		return OperationRecord{}, err
	}
	action := "deactivate"
	if enabled {
		action = "activate"
	}
	record := OperationRecord{ID: id, PluginID: "xray-manager", Action: action, Status: "succeeded", PreviousVersion: installation.Version, TargetVersion: installation.Version, CreatedAt: h.now().UTC()}
	if err := h.operations.Put(ctx, record); err != nil {
		return OperationRecord{}, err
	}
	return record, nil
}

func (h *ManagementHTTP) uninstall(w http.ResponseWriter, r *http.Request) {
	principal, ok := LifecyclePrincipalFromContext(r.Context())
	if !ok {
		writePluginProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return
	}
	if _, ok := pluginID(w, r); !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	installation, err := h.lifecycle.Get(r.Context())
	if err != nil {
		writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_SOURCE_UNAVAILABLE")
		return
	}
	if !installation.Installed || installation.Phase == "uninstalled" {
		writePluginProblem(w, http.StatusNotFound, "PLUGIN_NOT_FOUND")
		return
	}
	if installation.Enabled {
		writePluginProblem(w, http.StatusConflict, "PLUGIN_MUST_BE_DISABLED")
		return
	}
	operationID, err := randomOperationID()
	if err != nil {
		writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_OPERATION_UNAVAILABLE")
		return
	}
	removed, err := h.lifecycle.Uninstall(r.Context(), LifecycleOperation{
		ActorID: principal.SubjectID, TaskID: operationID, Action: "plugins.uninstall",
		Digest: installation.Digest, ExpectedRevision: installation.Revision,
	})
	if err != nil {
		writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_LIFECYCLE_UNAVAILABLE")
		return
	}
	record := OperationRecord{ID: operationID, PluginID: "xray-manager", Action: "uninstall", Status: "succeeded", PreviousVersion: installation.Version, TargetVersion: "", CreatedAt: h.now().UTC()}
	if err := h.operations.Put(r.Context(), record); err != nil {
		writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_OPERATION_UNAVAILABLE")
		return
	}
	_ = removed
	writePluginJSON(w, http.StatusOK, operationDTO(record))
}

func (h *ManagementHTTP) operation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("operationId")
	record, ok, err := h.operations.Get(r.Context(), id)
	if err != nil {
		writePluginProblem(w, http.StatusServiceUnavailable, "PLUGIN_OPERATION_UNAVAILABLE")
		return
	}
	if !ok {
		writePluginProblem(w, http.StatusNotFound, "PLUGIN_OPERATION_NOT_FOUND")
		return
	}
	writePluginJSON(w, http.StatusOK, operationDTO(record))
}
