package certificates

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

// Principal is supplied by Controller authentication middleware.
type Principal struct {
	SubjectID string
	RequestID string
}

// Authenticator resolves the trusted principal or returns an error (mapped to 401).
type Authenticator func(*http.Request) (Principal, error)

// GroupMembers snapshots service group members through the owner port. It is
// supplied by composition; certificates never reads service group storage.
type GroupMembers interface {
	Members(ctx context.Context, serviceGroupID string) ([]string, error)
}

// AuditActions returns the audit action catalog this domain exposes.
func AuditActions() []string {
	return append([]string(nil), ServiceActions()...)
}

type httpAPI struct {
	service       *Service
	groups        GroupMembers
	authenticator Authenticator
	now           func() time.Time
}

// RegisterHTTP mounts the internal certificate distribution endpoints. Response
// models never include key references or key material.
func RegisterHTTP(mux *http.ServeMux, service *Service, groups GroupMembers, authenticator Authenticator) error {
	if mux == nil {
		return errors.New("certificates: mux is required")
	}
	if service == nil {
		return errors.New("certificates: service is required")
	}
	if authenticator == nil {
		return errors.New("certificates: authenticator is required")
	}
	api := &httpAPI{service: service, groups: groups, authenticator: authenticator, now: service.now}
	mux.HandleFunc("GET /api/v1/certificates/internal/distributions", api.list)
	mux.HandleFunc("POST /api/v1/certificates/internal/distributions", api.create)
	mux.HandleFunc("POST /api/v1/certificates/internal/distributions/{distributionId}/revoke", api.revoke)
	return nil
}

type jobDTO struct {
	JobID                 string          `json:"jobId"`
	ServiceGroupID        string          `json:"serviceGroupId"`
	MemberNodeIDsSnapshot []string        `json:"memberNodeIdsSnapshot"`
	CredentialType        string          `json:"credentialType"`
	State                 string          `json:"state"`
	Results               []nodeResultDTO `json:"results"`
	CreatedAt             string          `json:"createdAt,omitempty"`
}

type nodeResultDTO struct {
	NodeID            string `json:"nodeId"`
	State             string `json:"state"`
	CertificateSerial string `json:"certificateSerial,omitempty"`
	ExpiresAt         string `json:"expiresAt,omitempty"`
	ErrorCode         string `json:"errorCode,omitempty"`
}

func (api *httpAPI) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	details, err := api.service.ListDetails(r.Context(), principal.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]jobDTO, 0, len(details))
	for _, detail := range details {
		if detail.Certificate.Kind != KindInternal {
			continue
		}
		items = append(items, jobFromDetail(detail))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createBody struct {
	ServiceGroupID        string `json:"serviceGroupId"`
	CredentialType        string `json:"credentialType"`
	IdentityProfile       string `json:"identityProfile"`
	ValidityHours         *int   `json:"validityHours"`
	ConfirmServiceImpact  bool   `json:"confirmServiceImpact"`
	ExpectedGroupRevision uint64 `json:"expectedGroupRevision"`
	KeyRef                string `json:"keyRef"`
}

func (api *httpAPI) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body createBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.ServiceGroupID == "" || body.CredentialType == "" || !body.ConfirmServiceImpact || body.ExpectedGroupRevision == 0 {
		writeFixed(w, http.StatusBadRequest, "invalid_request",
			"serviceGroupId, credentialType, confirmServiceImpact and expectedGroupRevision are required")
		return
	}
	credentialType := CredentialType(body.CredentialType)
	if !credentialType.valid() {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "credentialType must be trust_bundle or per_node_identity")
		return
	}
	if containsPrivateKeyMaterial(body.KeyRef) || containsPrivateKeyMaterial(body.IdentityProfile) {
		writeFixed(w, http.StatusBadRequest, "key_material_rejected", "private key material is not accepted")
		return
	}
	if api.groups == nil {
		writeFixed(w, http.StatusServiceUnavailable, "group_members_unavailable",
			"service group member resolution is not configured")
		return
	}
	nodes, err := api.groups.Members(r.Context(), body.ServiceGroupID)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(nodes) == 0 {
		writeFixed(w, http.StatusUnprocessableEntity, "empty_group", "service group has no member nodes")
		return
	}
	hours := 720
	if body.ValidityHours != nil {
		hours = *body.ValidityHours
	}
	if hours < 1 || hours > 8760 {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "validityHours must be within 1-8760")
		return
	}
	// The adapter-generated common name carries the distribution mode so GET can
	// reconstruct it without a schema change.
	commonName := fmt.Sprintf("%s.%s.internal", body.ServiceGroupID, credentialType)
	keyRef := body.KeyRef
	if keyRef == "" {
		keyRef = newKeyReference()
	}
	now := api.now().UTC()
	result, err := api.service.CreateInternalDistribution(r.Context(), principal.SubjectID, principal.RequestID, CreateInternalDistributionInput{
		ID: newCertificateID(), CommonName: commonName, ServiceGroupID: body.ServiceGroupID,
		NodeIDs: nodes, ExpectedGroupRevision: body.ExpectedGroupRevision,
		KeyRef: KeyRef(keyRef), Validity: Validity{NotBefore: now, NotAfter: now.Add(time.Duration(hours) * time.Hour)},
		CredentialType: credentialType, Confirm: true,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, jobFromDetail(CertificateDetail{
		Certificate: result.Certificate, Distributions: result.Distributions,
	}))
}

func (api *httpAPI) revoke(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason                string `json:"reason"`
		IncludeAllIssuedLeafs bool   `json:"includeAllIssuedLeafs"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validText(body.Reason, 300) {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "reason is required")
		return
	}
	certificateID := certificateIDFromPath(r.PathValue("distributionId"))
	if _, err := api.service.Revoke(r.Context(), principal.SubjectID, principal.RequestID, certificateID); err != nil {
		writeError(w, err)
		return
	}
	detail, err := api.service.Get(r.Context(), principal.SubjectID, certificateID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, jobFromDetail(detail))
}

func jobFromDetail(detail CertificateDetail) jobDTO {
	certificate := detail.Certificate
	results := make([]nodeResultDTO, 0, len(detail.Distributions))
	groupID := ""
	for _, distribution := range detail.Distributions {
		if groupID == "" {
			groupID = distribution.ServiceGroupID
		}
		result := nodeResultDTO{
			NodeID: distribution.NodeID, State: distributionState(distribution.Status),
			CertificateSerial: certificate.Serial, ExpiresAt: formatOptionalTime(certificate.NotAfter),
		}
		if distribution.Status == DistributionFailed && distribution.Error != "" {
			result.ErrorCode = "distribution_failed"
		}
		results = append(results, result)
	}
	return jobDTO{
		JobID: certificate.ID, ServiceGroupID: groupID,
		MemberNodeIDsSnapshot: resultNodeIDs(detail.Distributions),
		CredentialType:        credentialTypeOf(certificate), State: jobState(certificate, detail.Distributions),
		Results: results, CreatedAt: formatOptionalTime(certificate.CreatedAt),
	}
}

// credentialTypeOf reconstructs the distribution mode from the common name
// generated by this adapter. No separate credential-type column is persisted.
func credentialTypeOf(certificate CertificateView) string {
	if strings.HasSuffix(certificate.CommonName, "."+string(CredentialPerNodeIdentity)+".internal") {
		return string(CredentialPerNodeIdentity)
	}
	return string(CredentialTrustBundle)
}

func resultNodeIDs(distributions []DistributionView) []string {
	ids := make([]string, 0, len(distributions))
	for _, distribution := range distributions {
		ids = append(ids, distribution.NodeID)
	}
	return ids
}

func distributionState(status DistributionStatus) string {
	switch status {
	case DistributionPending:
		return "queued"
	case DistributionSubmitted:
		return "distributing"
	case DistributionActive:
		return "verified"
	case DistributionFailed:
		return "failed"
	case DistributionRevoked:
		return "revoked"
	default:
		return "failed"
	}
}

func jobState(certificate CertificateView, distributions []DistributionView) string {
	if certificate.Status == StatusRevoked {
		return "revoked"
	}
	pending, submitted, active, failed := 0, 0, 0, 0
	for _, distribution := range distributions {
		switch distribution.Status {
		case DistributionPending:
			pending++
		case DistributionSubmitted:
			submitted++
		case DistributionActive:
			active++
		case DistributionFailed:
			failed++
		case DistributionRevoked:
			return "revoked"
		}
	}
	switch {
	case len(distributions) == 0:
		return "queued"
	case pending == len(distributions):
		return "queued"
	case submitted > 0 || pending > 0:
		return "running"
	case failed > 0 && active > 0:
		return "partial"
	case failed == len(distributions):
		return "failed"
	default:
		return "completed"
	}
}

// distributionID is "len:certificateID:nodeID"; the path accepts either the job
// (certificate) id or the opaque distribution id returned by the service.
func certificateIDFromPath(raw string) string {
	parts := strings.SplitN(raw, ":", 3)
	if len(parts) != 3 {
		return raw
	}
	length, err := strconv.Atoi(parts[0])
	if err != nil || length <= 0 {
		return raw
	}
	// Rejoin in case the certificate id itself contains a colon.
	remainder := parts[1] + ":" + parts[2]
	if length > len(remainder) {
		return raw
	}
	candidate := remainder[:length]
	if candidate == "" {
		return raw
	}
	return candidate
}

func newCertificateID() string { return newHTTPID("cert") }

func newKeyReference() string { return newHTTPID("keyref") }

func newHTTPID(prefix string) string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		panic("certificates: entropy source failed")
	}
	return prefix + "_" + hex.EncodeToString(buffer)
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

func formatOptionalTime(value time.Time) string {
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
	case errors.Is(err, ErrCertificateNotFound):
		writeFixed(w, http.StatusNotFound, "not_found", "certificate distribution was not found")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrRevisionConflict), errors.Is(err, ErrCertificateExists), errors.Is(err, ErrDistributionExists),
		errors.Is(err, ErrCertificateRevoked):
		writeFixed(w, http.StatusConflict, "conflict", "resource state changed")
	case errors.Is(err, ErrKeyMaterialRejected):
		writeFixed(w, http.StatusBadRequest, "key_material_rejected", "private key material is not accepted")
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidKeyRef), errors.Is(err, ErrInvalidValidity),
		errors.Is(err, ErrConfirmationRequired), errors.Is(err, ErrInvalidRevision), errors.Is(err, ErrRequestIDRequired):
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	case errors.Is(err, ErrIssuerUnavailable), errors.Is(err, ErrTaskSubmitterUnavailable), errors.Is(err, ErrGroupRevisionsUnavailable):
		writeFixed(w, http.StatusServiceUnavailable, "certificate_dependency_unavailable", "certificate dependency is unavailable")
	case errors.Is(err, ErrIssuerFailed), errors.Is(err, ErrTaskSubmitFailed), errors.Is(err, ErrRevisionCheckFailed),
		errors.Is(err, ErrInvalidIssuerResponse), errors.Is(err, ErrInvalidTaskID):
		writeFixed(w, http.StatusBadGateway, "certificate_operation_failed", "certificate operation failed")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}
