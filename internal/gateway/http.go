package gateway

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
		return ErrInvalidGateway
	}
	api := &httpAPI{service: service, authenticator: authenticator}
	mux.HandleFunc("GET /api/v1/gateways", api.listGateways)
	mux.HandleFunc("POST /api/v1/gateways", api.createGateway)
	mux.HandleFunc("GET /api/v1/gateways/{gatewayId}", api.getGateway)
	mux.HandleFunc("PATCH /api/v1/gateways/{gatewayId}", api.updateGateway)
	mux.HandleFunc("POST /api/v1/gateways/{gatewayId}/provision", api.provision)
	mux.HandleFunc("GET /api/v1/gateway-provisions/{jobId}", api.getProvision)
	mux.HandleFunc("GET /api/v1/gateway-routes", api.listRoutes)
	mux.HandleFunc("POST /api/v1/gateway-routes", api.createRoute)
	mux.HandleFunc("PATCH /api/v1/gateway-routes/{routeId}", api.updateRoute)
	mux.HandleFunc("DELETE /api/v1/gateway-routes/{routeId}", api.deleteRoute)
	return nil
}

type gatewayDTO struct {
	ID                 string `json:"id"`
	NodeID             string `json:"nodeId"`
	Name               string `json:"name"`
	Region             string `json:"region,omitempty"`
	CountryCode        string `json:"countryCode,omitempty"`
	Online             bool   `json:"online"`
	NginxInstalled     bool   `json:"nginxInstalled"`
	ChecksPassed       bool   `json:"checksPassed"`
	Enabled            bool   `json:"enabled"`
	Status             string `json:"status"`
	SchedulingEligible bool   `json:"schedulingEligible"`
	LastError          string `json:"lastError,omitempty"`
	NginxVersion       string `json:"nginxVersion,omitempty"`
	Revision           uint64 `json:"revision,omitempty"`
}

type routeDTO struct {
	ID             string `json:"id"`
	GatewayID      string `json:"gatewayId,omitempty"`
	Domain         string `json:"domain"`
	Upstream       string `json:"upstream,omitempty"`
	ServiceGroupID string `json:"serviceGroupId,omitempty"`
	HTTPS          bool   `json:"https"`
	Enabled        bool   `json:"enabled"`
	Revision       uint64 `json:"revision,omitempty"`
}

type provisionDTO struct {
	ID        string           `json:"id"`
	NodeID    string           `json:"nodeId,omitempty"`
	GatewayID string           `json:"gatewayId,omitempty"`
	Status    string           `json:"status"`
	Phase     string           `json:"phase,omitempty"`
	CreatedAt string           `json:"createdAt"`
	Error     *gatewayErrorDTO `json:"error,omitempty"`
}

type gatewayErrorDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (api *httpAPI) listGateways(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	gateways, err := api.service.ListGateways(r.Context(), principal.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]gatewayDTO, 0, len(gateways))
	for _, gateway := range gateways {
		view, err := api.view(r.Context(), principal.SubjectID, gateway)
		if err != nil {
			writeError(w, err)
			return
		}
		items = append(items, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api *httpAPI) createGateway(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		NodeID                   string `json:"nodeId"`
		AcknowledgeInstallImpact bool   `json:"acknowledgeInstallImpact"`
		InstallIfMissing         *bool  `json:"installIfMissing"`
		PreserveExistingConfig   *bool  `json:"preserveExistingConfig"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.NodeID == "" || !body.AcknowledgeInstallImpact {
		writeFixed(w, http.StatusBadRequest, "invalid_request", "nodeId and install acknowledgement are required")
		return
	}
	gateway, err := api.service.CreateGateway(r.Context(), principal.SubjectID, principal.RequestID, CreateGatewayInput{
		ID: newGatewayID(), NodeID: body.NodeID, Name: body.NodeID, Enabled: false,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	preserve := true
	if body.PreserveExistingConfig != nil {
		preserve = *body.PreserveExistingConfig
	}
	provision, err := api.service.Provision(r.Context(), principal.SubjectID, principal.RequestID, gateway.ID, ProvisionInput{
		ExistingSite: !preserve,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, provisionFromRecord(provision, gateway.NodeID))
}

func (api *httpAPI) getGateway(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	gateway, err := api.service.GetGateway(r.Context(), principal.SubjectID, r.PathValue("gatewayId"))
	if err != nil {
		writeError(w, err)
		return
	}
	view, err := api.view(r.Context(), principal.SubjectID, gateway)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (api *httpAPI) updateGateway(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	current, err := api.service.GetGateway(r.Context(), principal.SubjectID, r.PathValue("gatewayId"))
	if err != nil {
		writeError(w, err)
		return
	}
	updated, err := api.service.UpdateGateway(r.Context(), principal.SubjectID, principal.RequestID, current.ID, current.Revision, UpdateGatewayInput{
		Name: current.Name, Region: current.Region, CountryCode: current.CountryCode, Enabled: body.Enabled,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	view, err := api.view(r.Context(), principal.SubjectID, updated)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (api *httpAPI) provision(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	gateway, err := api.service.GetGateway(r.Context(), principal.SubjectID, r.PathValue("gatewayId"))
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		ExistingSite     bool   `json:"existingSite"`
		TakeoverApproved bool   `json:"takeoverApproved"`
		ApprovalRef      string `json:"approvalRef"`
	}
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &body) {
			return
		}
	}
	provision, err := api.service.Provision(r.Context(), principal.SubjectID, principal.RequestID, gateway.ID, ProvisionInput{
		ExistingSite: body.ExistingSite, TakeoverApproved: body.TakeoverApproved, ApprovalRef: body.ApprovalRef,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, provisionFromRecord(provision, gateway.NodeID))
}

func (api *httpAPI) getProvision(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	provision, err := api.service.GetProvision(r.Context(), principal.SubjectID, r.PathValue("jobId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, provisionFromRecord(provision, ""))
}

func (api *httpAPI) listRoutes(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	gateways, err := api.service.ListGateways(r.Context(), principal.SubjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]routeDTO, 0)
	for _, gateway := range gateways {
		routes, err := api.service.ListRoutes(r.Context(), principal.SubjectID, gateway.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		for _, route := range routes {
			items = append(items, routeFromRecord(route))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api *httpAPI) createRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		GatewayID      string `json:"gatewayId"`
		Domain         string `json:"domain"`
		Upstream       string `json:"upstream"`
		ServiceGroupID string `json:"serviceGroupId"`
		HTTPS          bool   `json:"https"`
		Enabled        bool   `json:"enabled"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.GatewayID == "" {
		writeFixed(w, http.StatusBadRequest, "gateway_id_required", "gatewayId is required by this implementation")
		return
	}
	route, err := api.service.CreateRoute(r.Context(), principal.SubjectID, principal.RequestID, body.GatewayID, CreateRouteInput{
		ID: newRouteID(), Domain: body.Domain, Upstream: body.Upstream,
		ServiceGroupID: body.ServiceGroupID, HTTPS: body.HTTPS, Enabled: body.Enabled,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, routeFromRecord(route))
}

func (api *httpAPI) updateRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	current, err := api.service.GetRoute(r.Context(), principal.SubjectID, r.PathValue("routeId"))
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Domain         string `json:"domain"`
		Upstream       string `json:"upstream"`
		ServiceGroupID string `json:"serviceGroupId"`
		HTTPS          bool   `json:"https"`
		Enabled        bool   `json:"enabled"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	route, err := api.service.UpdateRoute(r.Context(), principal.SubjectID, principal.RequestID,
		current.GatewayID, current.ID, current.Revision, UpdateRouteInput{
			Domain: body.Domain, Upstream: body.Upstream, ServiceGroupID: body.ServiceGroupID,
			HTTPS: body.HTTPS, Enabled: body.Enabled,
		})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, routeFromRecord(route))
}

func (api *httpAPI) deleteRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	current, err := api.service.GetRoute(r.Context(), principal.SubjectID, r.PathValue("routeId"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := api.service.DeleteRoute(r.Context(), principal.SubjectID, principal.RequestID,
		current.GatewayID, current.ID, current.Revision); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *httpAPI) view(ctx context.Context, subjectID string, gateway GatewayNode) (gatewayDTO, error) {
	_, online, eligible, err := api.gatewayStatus(ctx, subjectID, gateway)
	if err != nil {
		return gatewayDTO{}, err
	}
	return gatewayDTO{
		ID: gateway.ID, NodeID: gateway.NodeID, Name: gateway.Name, Region: gateway.Region,
		CountryCode: gateway.CountryCode, Online: online, NginxInstalled: gateway.NginxInstalled,
		ChecksPassed: gateway.ChecksPassed, Enabled: gateway.Enabled, Status: string(gateway.Status),
		SchedulingEligible: eligible, LastError: gateway.LastError, NginxVersion: gateway.NginxVersion,
		Revision: gateway.Revision,
	}, nil
}

func (api *httpAPI) gatewayStatus(ctx context.Context, subjectID string, gateway GatewayNode) (GatewayNode, bool, bool, error) {
	node, err := api.service.lookupNode(ctx, gateway.NodeID)
	if err != nil {
		return gateway, false, false, err
	}
	return gateway, node.Online, gateway.SchedulingEligible(node.Online), nil
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

func routeFromRecord(route GatewayRoute) routeDTO {
	return routeDTO{
		ID: route.ID, GatewayID: route.GatewayID, Domain: route.Domain, Upstream: route.Upstream,
		ServiceGroupID: route.ServiceGroupID, HTTPS: route.HTTPS, Enabled: route.Enabled,
		Revision: route.Revision,
	}
}

func provisionFromRecord(provision Provision, nodeID string) provisionDTO {
	status := string(provision.Status)
	switch provision.Status {
	case ProvisionCompleted:
		status = "succeeded"
	case ProvisionRunning:
		status = "installing"
	}
	phase := ""
	if len(provision.Steps) > 0 {
		phase = provision.Steps[len(provision.Steps)-1].Name
	}
	dto := provisionDTO{
		ID: provision.ID, NodeID: nodeID, GatewayID: provision.GatewayID, Status: status,
		Phase: phase, CreatedAt: provision.CreatedAt.UTC().Format(time.RFC3339),
	}
	if provision.Error != "" {
		dto.Error = &gatewayErrorDTO{Code: "provision_failed", Message: provision.Error}
	}
	return dto
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
	case errors.Is(err, ErrGatewayNotFound), errors.Is(err, ErrRouteNotFound), errors.Is(err, ErrProvisionNotFound),
		errors.Is(err, ErrNodeNotFound):
		writeFixed(w, http.StatusNotFound, "not_found", "resource was not found")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeFixed(w, http.StatusRequestTimeout, "request_timeout", "request was interrupted")
	case errors.Is(err, ErrGatewayExists), errors.Is(err, ErrRevisionConflict), errors.Is(err, ErrRouteExists),
		errors.Is(err, ErrRouteDomainTaken), errors.Is(err, ErrProvisionExists), errors.Is(err, ErrProvisionInProgress),
		errors.Is(err, ErrProvisionTerminal), errors.Is(err, ErrProvisionConflict):
		writeFixed(w, http.StatusConflict, "conflict", "resource state changed")
	case errors.Is(err, ErrTakeoverApprovalRequired):
		writeFixed(w, http.StatusConflict, "takeover_approval_required", "existing site takeover requires approval")
	case errors.Is(err, ErrRouteCheckFailed), errors.Is(err, ErrRoutePortConflict):
		writeFixed(w, http.StatusUnprocessableEntity, "route_check_failed", "route health or port check failed")
	case errors.Is(err, ErrCheckerUnavailable), errors.Is(err, ErrTaskSubmitterUnavailable), errors.Is(err, ErrNodeLookupFailed),
		errors.Is(err, ErrServiceGroupLookupFailed):
		writeFixed(w, http.StatusServiceUnavailable, "gateway_unavailable", "gateway dependency is unavailable")
	case errors.Is(err, ErrTaskSubmitFailed):
		writeFixed(w, http.StatusBadGateway, "task_submit_failed", "unified task submission failed")
	case errors.Is(err, ErrInvalidGateway), errors.Is(err, ErrInvalidRoute), errors.Is(err, ErrInvalidRevision),
		errors.Is(err, ErrRequestIDRequired), errors.Is(err, ErrServiceGroupNotFound), errors.Is(err, ErrInvalidProvision),
		errors.Is(err, ErrProvisionTransition):
		writeFixed(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	default:
		writeFixed(w, http.StatusInternalServerError, "internal_error", "request failed")
	}
}

func newGatewayID() string { return newHTTPID("gw") }
func newRouteID() string   { return newHTTPID("route") }

func newHTTPID(prefix string) string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		panic("gateway: entropy source failed")
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}
