package plugins

import (
	"context"
	"net/http"
	"strings"
)

// XrayDeclarativeHTTP is the declarative bridge for the approved optional
// xray-manager package. It imports no xray business code and never starts Xray,
// executes a shell, reads a private key or fabricates an instance observation.
type XrayNodeView struct {
	NodeID string
	Name   string
	Online bool
}

type XrayDeclarativeHTTP struct {
	authenticate PluginAuthenticator
	state        func(context.Context) (Installation, error)
	listNodes    func(context.Context) ([]XrayNodeView, error)
	mux          *http.ServeMux
}

func NewXrayDeclarativeHTTP(authenticate PluginAuthenticator, state func(context.Context) (Installation, error), listNodes func(context.Context) ([]XrayNodeView, error)) (*XrayDeclarativeHTTP, error) {
	if authenticate == nil || state == nil {
		return nil, ErrPackageRejected
	}
	h := &XrayDeclarativeHTTP{authenticate: authenticate, state: state, listNodes: listNodes, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /api/v1/plugins/xray-manager", h.status)
	h.mux.HandleFunc("GET /api/v1/plugins/xray-manager/capabilities", h.capabilities)
	h.mux.HandleFunc("GET /api/v1/plugins/xray-manager/nodes", h.nodes)
	h.mux.HandleFunc("GET /api/v1/plugins/xray-manager/nodes/{nodeId}/instance", h.instance)
	h.mux.HandleFunc("POST /api/v1/plugins/xray-manager/nodes/{nodeId}/instance/actions", h.unavailableRuntime)
	h.mux.HandleFunc("GET /api/v1/plugins/xray-manager/templates", h.templates)
	h.mux.HandleFunc("POST /api/v1/plugins/xray-manager/installations/preview", h.unavailablePackage)
	h.mux.HandleFunc("POST /api/v1/plugins/xray-manager/installations", h.unavailablePackage)
	h.mux.HandleFunc("POST /api/v1/plugins/xray-manager/configurations/validate", h.unavailableValidation)
	h.mux.HandleFunc("POST /api/v1/plugins/xray-manager/deployments/preview", h.unavailableRuntime)
	h.mux.HandleFunc("POST /api/v1/plugins/xray-manager/deployments", h.unavailableRuntime)
	h.mux.HandleFunc("GET /api/v1/plugins/xray-manager/nodes/{nodeId}/configurations", h.configurations)
	h.mux.HandleFunc("POST /api/v1/plugins/xray-manager/nodes/{nodeId}/configurations/rollback", h.unavailableRuntime)
	h.mux.HandleFunc("GET /api/v1/plugins/xray-manager/users", h.users)
	h.mux.HandleFunc("POST /api/v1/plugins/xray-manager/users", h.unavailableRuntime)
	h.mux.HandleFunc("PATCH /api/v1/plugins/xray-manager/users/{userId}", h.unavailableRuntime)
	h.mux.HandleFunc("DELETE /api/v1/plugins/xray-manager/users/{userId}", h.unavailableRuntime)
	h.mux.HandleFunc("POST /api/v1/plugins/xray-manager/users/{userId}/client-export", h.unavailableExport)
	h.mux.HandleFunc("GET /api/v1/plugins/xray-manager/traffic", h.traffic)
	h.mux.HandleFunc("GET /api/v1/plugins/xray-manager/traffic/settings", h.unavailableRuntime)
	h.mux.HandleFunc("PUT /api/v1/plugins/xray-manager/traffic/settings", h.unavailableRuntime)
	h.mux.HandleFunc("GET /api/v1/plugins/xray-manager/jobs/{jobId}", h.jobNotFound)
	return h, nil
}

// Patterns returns the exact method+path patterns this handler owns.
func (h *XrayDeclarativeHTTP) Patterns() []string {
	return []string{
		"GET /api/v1/plugins/xray-manager",
		"GET /api/v1/plugins/xray-manager/capabilities",
		"GET /api/v1/plugins/xray-manager/nodes",
		"GET /api/v1/plugins/xray-manager/nodes/{nodeId}/instance",
		"POST /api/v1/plugins/xray-manager/nodes/{nodeId}/instance/actions",
		"GET /api/v1/plugins/xray-manager/templates",
		"POST /api/v1/plugins/xray-manager/installations/preview",
		"POST /api/v1/plugins/xray-manager/installations",
		"POST /api/v1/plugins/xray-manager/configurations/validate",
		"POST /api/v1/plugins/xray-manager/deployments/preview",
		"POST /api/v1/plugins/xray-manager/deployments",
		"GET /api/v1/plugins/xray-manager/nodes/{nodeId}/configurations",
		"POST /api/v1/plugins/xray-manager/nodes/{nodeId}/configurations/rollback",
		"GET /api/v1/plugins/xray-manager/users",
		"POST /api/v1/plugins/xray-manager/users",
		"PATCH /api/v1/plugins/xray-manager/users/{userId}",
		"DELETE /api/v1/plugins/xray-manager/users/{userId}",
		"POST /api/v1/plugins/xray-manager/users/{userId}/client-export",
		"GET /api/v1/plugins/xray-manager/traffic",
		"GET /api/v1/plugins/xray-manager/traffic/settings",
		"PUT /api/v1/plugins/xray-manager/traffic/settings",
		"GET /api/v1/plugins/xray-manager/jobs/{jobId}",
	}
}

func (h *XrayDeclarativeHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	h.mux.ServeHTTP(w, r)
}

func (h *XrayDeclarativeHTTP) enabledState(w http.ResponseWriter, r *http.Request) (Installation, bool) {
	installation, err := h.state(r.Context())
	if err != nil {
		writePluginProblem(w, http.StatusServiceUnavailable, "XRAY_PLUGIN_STATE_UNAVAILABLE")
		return Installation{}, false
	}
	if !installation.Installed || installation.Phase == "uninstalled" {
		writePluginProblem(w, http.StatusForbidden, "XRAY_PLUGIN_NOT_INSTALLED")
		return Installation{}, false
	}
	if !installation.Enabled {
		writePluginProblem(w, http.StatusForbidden, "XRAY_PLUGIN_DISABLED")
		return Installation{}, false
	}
	return installation, true
}

func xrayCapabilities() []string {
	return []string{"xray.read", "xray.config", "xray.users", "xray.traffic", "xray.export"}
}

func (h *XrayDeclarativeHTTP) status(w http.ResponseWriter, r *http.Request) {
	installation, ok := h.enabledState(w, r)
	if !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	writePluginJSON(w, http.StatusOK, map[string]any{
		"id": "xray-manager", "version": installation.Version, "enabled": true, "status": installation.Phase,
		"declarative": true, "capabilities": xrayCapabilities(),
	})
}

func (h *XrayDeclarativeHTTP) capabilities(w http.ResponseWriter, r *http.Request) {
	installation, ok := h.enabledState(w, r)
	if !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	writePluginJSON(w, http.StatusOK, map[string]any{"id": "xray-manager", "version": installation.Version, "capabilities": xrayCapabilities(), "declarative": true})
}

func (h *XrayDeclarativeHTTP) nodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.enabledState(w, r); !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	items := []map[string]any{}
	if h.listNodes != nil {
		nodes, err := h.listNodes(r.Context())
		if err != nil {
			writePluginProblem(w, http.StatusServiceUnavailable, "XRAY_SOURCE_UNAVAILABLE")
			return
		}
		for _, node := range nodes {
			items = append(items, map[string]any{"nodeId": node.NodeID, "name": node.Name, "online": node.Online, "managed": false})
		}
	}
	writePluginJSON(w, http.StatusOK, map[string]any{"items": items})
}

func safeXrayNodeID(id string) bool {
	return id != "" && len(id) <= 128 && !strings.ContainsAny(id, "/\\*?[] \t\r\n\x00")
}

func (h *XrayDeclarativeHTTP) instance(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.enabledState(w, r); !ok {
		return
	}
	nodeID := r.PathValue("nodeId")
	if !safeXrayNodeID(nodeID) {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	// No Agent/Xray runtime observation exists in this profile. The honest
	// service status is unknown rather than "stopped" or "running".
	writePluginJSON(w, http.StatusOK, map[string]any{"nodeId": nodeID, "managed": false, "serviceStatus": "unknown"})
}

func (h *XrayDeclarativeHTTP) templates(w http.ResponseWriter, r *http.Request) {
	installation, ok := h.enabledState(w, r)
	if !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	version := installation.Version
	writePluginJSON(w, http.StatusOK, map[string]any{"items": []any{
		map[string]any{"templateId": "vless-tls-vision", "name": "VLESS TLS Vision", "flow": "xtls-rprx-vision", "security": "tls", "supportedVersions": []string{version}, "requiredParameters": []string{"domain", "certificateRef"}},
		map[string]any{"templateId": "vless-reality-vision", "name": "VLESS Reality Vision", "flow": "xtls-rprx-vision", "security": "reality", "supportedVersions": []string{version}, "requiredParameters": []string{"serverName", "realityTarget", "realityKeyRef"}},
		map[string]any{"templateId": "custom-json", "name": "Custom JSON", "security": "custom", "supportedVersions": []string{version}, "requiredParameters": []string{"document"}},
	}})
}

func (h *XrayDeclarativeHTTP) configurations(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.enabledState(w, r); !ok {
		return
	}
	if !safeXrayNodeID(r.PathValue("nodeId")) {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	writePluginJSON(w, http.StatusOK, map[string]any{"items": []any{}})
}

func (h *XrayDeclarativeHTTP) users(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.enabledState(w, r); !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	writePluginJSON(w, http.StatusOK, map[string]any{"items": []any{}})
}

func (h *XrayDeclarativeHTTP) traffic(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.enabledState(w, r); !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writePluginProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	writePluginJSON(w, http.StatusOK, map[string]any{
		"items": []any{}, "sourceType": "proxy_only",
		"notice": "No authenticated Xray StatsService observation is available in this runtime profile.",
	})
}

func (h *XrayDeclarativeHTTP) unavailableRuntime(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.enabledState(w, r); !ok {
		return
	}
	writePluginProblem(w, http.StatusNotImplemented, "XRAY_RUNTIME_UNAVAILABLE")
}

func (h *XrayDeclarativeHTTP) unavailablePackage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.enabledState(w, r); !ok {
		return
	}
	writePluginProblem(w, http.StatusNotImplemented, "XRAY_PACKAGE_RUNTIME_UNAVAILABLE")
}

func (h *XrayDeclarativeHTTP) unavailableValidation(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.enabledState(w, r); !ok {
		return
	}
	writePluginProblem(w, http.StatusNotImplemented, "XRAY_VALIDATION_SOURCE_UNAVAILABLE")
}

func (h *XrayDeclarativeHTTP) unavailableExport(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.enabledState(w, r); !ok {
		return
	}
	writePluginProblem(w, http.StatusNotImplemented, "XRAY_MATERIAL_SOURCE_UNAVAILABLE")
}

func (h *XrayDeclarativeHTTP) jobNotFound(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.enabledState(w, r); !ok {
		return
	}
	writePluginProblem(w, http.StatusNotFound, "XRAY_JOB_NOT_FOUND")
}
