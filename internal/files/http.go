package files

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
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
	if mux == nil {
		return errors.New("files: mux is required")
	}
	if service == nil {
		return errors.New("files: service is required")
	}
	if authenticator == nil {
		return errors.New("files: authenticator is required")
	}
	api := &httpAPI{service: service, authenticator: authenticator}
	mux.HandleFunc("GET /api/v1/transfers", api.list)
	mux.HandleFunc("POST /api/v1/transfers", api.create)
	mux.HandleFunc("GET /api/v1/transfers/{transferId}", api.get)
	mux.HandleFunc("POST /api/v1/transfers/{transferId}/cancel", api.cancel)
	mux.HandleFunc("POST /api/v1/nodes/{nodeId}/files/uploads", api.createUpload)
	mux.HandleFunc("GET /api/v1/nodes/{nodeId}/files/uploads/{uploadId}", api.getUpload)
	mux.HandleFunc("PUT /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/content", api.uploadContent)
	mux.HandleFunc("POST /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/complete", api.completeUpload)
	mux.HandleFunc("POST /api/v1/files/distributions", api.createDistribution)
	mux.HandleFunc("GET /api/v1/files/distributions/{distributionId}", api.getDistribution)
	mux.HandleFunc("PUT /api/v1/files/distributions/{distributionId}/content", api.distributionContent)
	mux.HandleFunc("POST /api/v1/files/distributions/{distributionId}/complete", api.completeDistribution)
	mux.HandleFunc("POST /api/v1/files/distributions/{distributionId}/cancel", api.cancelDistribution)
	return nil
}

type transferDTO struct {
	TaskID            string    `json:"taskId,omitempty"`
	ID                string    `json:"id"`
	SourceNodeID      string    `json:"sourceNodeId"`
	SourcePaths       []string  `json:"sourcePaths"`
	DestinationNodeID string    `json:"destinationNodeId"`
	DestinationDir    string    `json:"destinationDir"`
	ConflictPolicy    string    `json:"conflictPolicy"`
	Status            string    `json:"status"`
	ProgressPercent   float64   `json:"progressPercent"`
	TotalBytes        *int64    `json:"totalBytes"`
	CopiedBytes       int64     `json:"copiedBytes"`
	Items             []itemDTO `json:"items"`
	CreatedAt         string    `json:"createdAt"`
	StartedAt         string    `json:"startedAt,omitempty"`
	CompletedAt       string    `json:"completedAt,omitempty"`
	Error             *errorDTO `json:"error,omitempty"`
}

type itemDTO struct {
	SourcePath      string    `json:"sourcePath"`
	DestinationPath string    `json:"destinationPath,omitempty"`
	Status          string    `json:"status"`
	BytesCopied     int64     `json:"bytesCopied"`
	Error           *errorDTO `json:"error,omitempty"`
}

type errorDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (api *httpAPI) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	transfers, err := api.service.ListVisibleTransfers(r.Context(), principal.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	statusFilter := r.URL.Query().Get("status")
	items := make([]transferDTO, 0, len(transfers))
	for _, transfer := range transfers {
		if statusFilter != "" && string(transfer.Status) != statusFilter {
			continue
		}
		items = append(items, dtoFromTransfer(transfer))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func (api *httpAPI) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key is required")
		return
	}
	var body struct {
		SourceNodeID      string            `json:"sourceNodeId"`
		SourcePaths       []string          `json:"sourcePaths"`
		DestinationNodeID string            `json:"destinationNodeId"`
		DestinationDir    string            `json:"destinationDir"`
		ConflictPolicy    ConflictPolicy    `json:"conflictPolicy"`
		FollowSymlinks    bool              `json:"followSymlinks"`
		DryRun            bool              `json:"dryRun"`
		Checksums         map[string]string `json:"checksums"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.DryRun {
		writeFixed(w, http.StatusBadRequest, "dry_run_unsupported", "dry run is available only through preflight")
		return
	}
	transfer, err := api.service.CreateTransfer(r.Context(), CreateTransferInput{
		SubjectID: principal.SubjectID, RequestID: principal.RequestID,
		SourceNodeID: body.SourceNodeID, DestinationNodeID: body.DestinationNodeID,
		SourcePaths: body.SourcePaths, DestinationDir: body.DestinationDir,
		ConflictPolicy: body.ConflictPolicy, FollowSymlinks: body.FollowSymlinks,
		DryRun: body.DryRun, Checksums: body.Checksums, IdempotencyKey: key,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, dtoFromTransfer(transfer))
}

func (api *httpAPI) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	transfer, err := api.service.GetTransfer(r.Context(), principal.SubjectID, r.PathValue("transferId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dtoFromTransfer(transfer))
}

func (api *httpAPI) cancel(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	transfer, err := api.service.CancelTransfer(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("transferId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, dtoFromTransfer(transfer))
}

func (api *httpAPI) createUpload(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		DestinationDir string         `json:"destinationDir"`
		ConflictPolicy ConflictPolicy `json:"conflictPolicy"`
		Files          []struct {
			Name      string `json:"name"`
			SizeBytes int64  `json:"sizeBytes"`
			SHA256    string `json:"sha256"`
		} `json:"files"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Files) != 1 {
		writeFixed(w, http.StatusBadRequest, "unsupported_batch_upload", "one file per upload session is supported")
		return
	}
	file := body.Files[0]
	destination, err := JoinRemotePath(body.DestinationDir, file.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	size := file.SizeBytes
	upload, err := api.service.CreateUpload(r.Context(), principal.SubjectID, principal.RequestID,
		r.PathValue("nodeId"), destination, &size, file.SHA256)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"uploadId": upload.ID, "status": "awaiting_data",
	})
}

func (api *httpAPI) getUpload(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	upload, err := api.service.GetUpload(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("uploadId"))
	if err != nil {
		writeError(w, err)
		return
	}
	if upload.NodeID != r.PathValue("nodeId") {
		writeFixed(w, http.StatusNotFound, "not_found", "upload was not found")
		return
	}
	projection := uploadProgressDTO(upload)
	binding := r.URL.Query()["resumeBinding"]
	if len(binding) > 1 || (len(binding) == 1 && binding[0] != "true" && binding[0] != "false") {
		writeFixed(w, http.StatusBadRequest, "invalid_query", "resumeBinding must be one boolean")
		return
	}
	if len(binding) == 1 && binding[0] == "true" {
		projection["destinationPath"] = upload.DestinationPath
		if upload.SHA256Expected != "" {
			projection["sha256Expected"] = upload.SHA256Expected
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, projection)
}

func (api *httpAPI) uploadContent(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	upload, err := api.service.GetUpload(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("uploadId"))
	if err != nil {
		writeError(w, err)
		return
	}
	if upload.NodeID != r.PathValue("nodeId") {
		writeFixed(w, http.StatusNotFound, "not_found", "upload was not found")
		return
	}
	offset, ok := uploadOffset(w, r)
	if !ok {
		return
	}
	if offset != upload.BytesReceived {
		writeFixed(w, http.StatusConflict, "conflict", "upload offset does not match the received byte count")
		return
	}
	if r.ContentLength > 0 {
		remaining := api.service.UploadLimit() - offset
		if upload.ExpectedSize != nil && *upload.ExpectedSize-offset < remaining {
			remaining = *upload.ExpectedSize - offset
		}
		if remaining < 0 || r.ContentLength > remaining {
			writeFixed(w, http.StatusRequestEntityTooLarge, "upload_too_large", "content exceeds the declared upload size")
			return
		}
	}
	updated, err := api.service.WriteUploadContent(r.Context(), principal.SubjectID, principal.RequestID, upload.ID, offset, r.Body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, uploadProgressDTO(updated))
}

func uploadOffset(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("offset")
	if raw == "" {
		raw = r.Header.Get("X-Upload-Offset")
	}
	if raw == "" {
		return 0, true
	}
	offset, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || offset < 0 {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "offset must be a non-negative integer")
		return 0, false
	}
	return offset, true
}

func uploadProgressDTO(upload Upload) map[string]any {
	var expected any
	if upload.ExpectedSize != nil {
		expected = *upload.ExpectedSize
	}
	status := "awaiting_data"
	if upload.Status != UploadOpen {
		// Non-open uploads expose their real state, including the unified
		// task dispatch and terminal outcomes.
		status = upload.Status
	}
	dto := map[string]any{
		"uploadId": upload.ID, "status": status, "bytesReceived": upload.BytesReceived,
		"expectedSize": expected,
	}
	if upload.TaskID != "" {
		dto["taskId"] = upload.TaskID
	}
	return dto
}

func (api *httpAPI) completeUpload(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	upload, err := api.service.GetUpload(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("uploadId"))
	if err != nil {
		writeError(w, err)
		return
	}
	if upload.NodeID != r.PathValue("nodeId") {
		writeFixed(w, http.StatusNotFound, "not_found", "upload was not found")
		return
	}
	completed, err := api.service.CompleteUpload(r.Context(), principal.SubjectID, principal.RequestID, upload.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	files := []map[string]any{{
		"name": baseName(completed.DestinationPath), "path": completed.DestinationPath,
		"type": "file", "sizeBytes": completed.BytesReceived,
		"version": completed.SHA256Actual,
	}}
	status := http.StatusOK
	if completed.Status == UploadDispatching {
		// The verified upload is accepted into the unified task queue. The
		// frame-authorized push, approval and the terminal state are owned by
		// the task worker; this response claims no transport success.
		status = http.StatusAccepted
	}
	dto := map[string]any{
		"uploadId": completed.ID, "status": completed.Status, "files": files,
	}
	if completed.TaskID != "" {
		dto["taskId"] = completed.TaskID
	}
	writeJSON(w, status, dto)
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

func dtoFromTransfer(transfer Transfer) transferDTO {
	sources := make([]string, 0, len(transfer.Items))
	items := make([]itemDTO, 0, len(transfer.Items))
	for _, item := range transfer.Items {
		sources = append(sources, item.SourcePath)
		dto := itemDTO{
			SourcePath: item.SourcePath, DestinationPath: item.DestinationPath,
			Status: string(item.Status), BytesCopied: item.BytesCopied,
		}
		if item.ErrorCode != "" || item.ErrorMessage != "" {
			dto.Error = &errorDTO{Code: item.ErrorCode, Message: item.ErrorMessage}
		}
		items = append(items, dto)
	}
	dto := transferDTO{
		TaskID: transfer.TaskID,
		ID:     transfer.ID, SourceNodeID: transfer.SourceNodeID, SourcePaths: sources,
		DestinationNodeID: transfer.DestinationNodeID, DestinationDir: transfer.DestinationDir,
		ConflictPolicy: string(transfer.ConflictPolicy), Status: string(transfer.Status),
		ProgressPercent: transfer.Progress, TotalBytes: transfer.TotalBytes,
		CopiedBytes: transfer.CopiedBytes, Items: items,
		CreatedAt: formatTime(transfer.CreatedAt),
	}
	if transfer.ErrorMessage != "" {
		dto.Error = &errorDTO{Code: "transfer_failed", Message: transfer.ErrorMessage}
	}
	if !transfer.StartedAt.IsZero() {
		dto.StartedAt = formatTime(transfer.StartedAt)
	}
	if !transfer.CompletedAt.IsZero() {
		dto.CompletedAt = formatTime(transfer.CompletedAt)
	}
	return dto
}

func formatTime(value time.Time) string {
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
	case errors.Is(err, auth.ErrDenied):
		writeFixed(w, http.StatusForbidden, "forbidden", "request is not permitted")
	case errors.Is(err, ErrTransferNotFound), errors.Is(err, ErrUploadNotFound), errors.Is(err, ErrDistributionNotFound):
		writeFixed(w, http.StatusNotFound, "not_found", "resource was not found")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrOffsetConflict), errors.Is(err, ErrUploadClosed), errors.Is(err, ErrDistributionClosed), errors.Is(err, ErrSubjectMismatch):
		writeFixed(w, http.StatusConflict, "conflict", "upload state changed")
	case errors.Is(err, ErrChecksumMismatch):
		writeFixed(w, http.StatusUnprocessableEntity, "checksum_mismatch", "uploaded file failed verification")
	case errors.Is(err, ErrUploadTooLarge):
		writeFixed(w, http.StatusRequestEntityTooLarge, "upload_too_large", "upload exceeds the size limit")
	case errors.Is(err, ErrTransportAbsent), errors.Is(err, ErrTaskSubmitter), errors.Is(err, ErrCancellerAbsent):
		writeFixed(w, http.StatusServiceUnavailable, "unavailable", "file transport is unavailable")
	case errors.Is(err, ErrSameNode):
		writeFixed(w, http.StatusConflict, "same_node", "source and destination nodes must differ")
	case errors.Is(err, ErrInvalidPath), errors.Is(err, ErrSymlinkForbidden), errors.Is(err, ErrInvalidTransfer),
		errors.Is(err, ErrInvalidTargets), errors.Is(err, ErrRequestIDRequired), errors.Is(err, ErrMissingAuthorizer):
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	case errors.Is(err, ErrTransportFailure):
		writeFixed(w, http.StatusBadGateway, "transport_failure", "node file transport failed")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}

func (api *httpAPI) createDistribution(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		DestinationDir string   `json:"destinationDir"`
		TargetNodes    []string `json:"targetNodes"`
		Files          []struct {
			Name      string `json:"name"`
			SizeBytes int64  `json:"sizeBytes"`
			SHA256    string `json:"sha256"`
		} `json:"files"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Files) != 1 {
		writeFixed(w, http.StatusBadRequest, "unsupported_batch_upload", "one file per distribution session is supported")
		return
	}
	file := body.Files[0]
	destination, err := JoinRemotePath(body.DestinationDir, file.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	size := file.SizeBytes
	distribution, err := api.service.CreateDistribution(r.Context(), principal.SubjectID, principal.RequestID,
		body.TargetNodes, destination, &size, file.SHA256)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"distributionId": distribution.ID, "status": "awaiting_data",
	})
}

func (api *httpAPI) getDistribution(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	distribution, err := api.service.GetDistribution(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("distributionId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, distributionDTO(distribution))
}

func (api *httpAPI) distributionContent(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	distribution, err := api.service.GetDistribution(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("distributionId"))
	if err != nil {
		writeError(w, err)
		return
	}
	if distribution.Status != DistributionOpen {
		writeError(w, ErrDistributionClosed)
		return
	}
	offset, ok := uploadOffset(w, r)
	if !ok {
		return
	}
	if offset != distribution.BytesReceived {
		writeFixed(w, http.StatusConflict, "conflict", "distribution offset does not match the received byte count")
		return
	}
	if r.ContentLength > 0 {
		remaining := api.service.UploadLimit() - offset
		if distribution.ExpectedSize != nil && *distribution.ExpectedSize-offset < remaining {
			remaining = *distribution.ExpectedSize - offset
		}
		if remaining < 0 || r.ContentLength > remaining {
			writeFixed(w, http.StatusRequestEntityTooLarge, "upload_too_large", "content exceeds the declared distribution size")
			return
		}
	}
	updated, err := api.service.WriteDistributionContent(r.Context(), principal.SubjectID, principal.RequestID, distribution.ID, offset, r.Body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, distributionDTO(updated))
}

func (api *httpAPI) completeDistribution(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	completed, err := api.service.CompleteDistribution(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("distributionId"))
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if completed.Status == DistributionDispatching {
		// The verified distribution is accepted into the unified task queue.
		// The frame-authorized per-target pushes and the terminal states are
		// owned by the task worker; this response claims no transport success.
		status = http.StatusAccepted
	}
	dto := distributionDTO(completed)
	if completed.TaskID != "" {
		dto["taskId"] = completed.TaskID
	}
	writeJSON(w, status, dto)
}

func (api *httpAPI) cancelDistribution(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	distribution, err := api.service.CancelDistribution(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("distributionId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, distributionDTO(distribution))
}

func distributionDTO(distribution Distribution) map[string]any {
	targets := make([]map[string]any, 0, len(distribution.Targets))
	for _, target := range distribution.Targets {
		item := map[string]any{
			"nodeId": target.NodeID, "status": target.Status, "bytes": target.Bytes,
		}
		if target.SHA256 != "" {
			item["sha256"] = target.SHA256
		}
		if target.ErrorCode != "" {
			item["error"] = map[string]string{"code": target.ErrorCode, "message": target.ErrorMessage}
		}
		targets = append(targets, item)
	}
	dto := map[string]any{
		"distributionId": distribution.ID, "status": distribution.Status,
		"destination": distribution.DestinationPath, "targetNodes": targets,
		"bytesReceived": distribution.BytesReceived,
	}
	if distribution.TaskID != "" {
		dto["taskId"] = distribution.TaskID
	}
	if distribution.ExpectedSize != nil {
		dto["sizeBytes"] = *distribution.ExpectedSize
	}
	if distribution.SHA256Actual != "" {
		dto["sha256"] = distribution.SHA256Actual
	}
	if distribution.ErrorCode != "" {
		dto["error"] = map[string]string{"code": distribution.ErrorCode, "message": distribution.ErrorCode}
	}
	return dto
}
