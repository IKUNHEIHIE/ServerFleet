package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

// XrayTaskRequest is a frozen, declarative invocation. This is not a WASM ABI
// or a runtime selection. Host registration needs separate runtime/isolation
// admission; its Submit MUST call the sole Controller task service and repeat
// current auth/approval/node-target checks. Core imports no optional business.
type XrayTaskRequest struct {
	Principal                                                   LifecyclePrincipal
	Installation                                                Installation
	Operation, NodeID, UserID, IdempotencyKey, ParametersSHA256 string
	Parameters                                                  json.RawMessage
}
type XrayTaskHost struct {
	Admit  func(context.Context, XrayTaskRequest) error
	Submit func(context.Context, XrayTaskRequest) (string, error)
}

// ConfigureXrayTaskHost returns a separately constructed handler. No runtime is
// enabled by this call: the existing installed+enabled lifecycle gate remains.
func NewXrayHTTPWithTaskHost(authenticate PluginAuthenticator, state func(context.Context) (Installation, error), listNodes func(context.Context) ([]XrayNodeView, error), host XrayTaskHost) (*XrayDeclarativeHTTP, error) {
	if host.Admit == nil || host.Submit == nil {
		return nil, ErrPackageRejected
	}
	h, err := NewXrayDeclarativeHTTP(authenticate, state, listNodes)
	if err != nil {
		return nil, err
	}
	h.taskHost = &host
	return h, nil
}
func cloneXrayTask(r XrayTaskRequest) XrayTaskRequest {
	r.Parameters = bytes.Clone(r.Parameters)
	return r
}
func (h *XrayDeclarativeHTTP) submitHostTask(w http.ResponseWriter, r *http.Request) bool {
	operations := map[string]string{
		"POST /api/v1/plugins/xray-manager/nodes/{nodeId}/instance/actions":        "xray.instance.action",
		"POST /api/v1/plugins/xray-manager/deployments":                            "xray.deploy",
		"POST /api/v1/plugins/xray-manager/nodes/{nodeId}/configurations/rollback": "xray.rollback",
		"POST /api/v1/plugins/xray-manager/users":                                  "xray.user.create",
		"PATCH /api/v1/plugins/xray-manager/users/{userId}":                        "xray.user.update",
		"DELETE /api/v1/plugins/xray-manager/users/{userId}":                       "xray.user.delete",
		"PUT /api/v1/plugins/xray-manager/traffic/settings":                        "xray.traffic.settings",
	}
	operation := operations[r.Pattern]
	if operation == "" {
		return false
	}
	installation, ok := h.enabledState(w, r)
	if !ok {
		return true
	}
	principal, ok := LifecyclePrincipalFromContext(r.Context())
	if !ok {
		writePluginProblem(w, 403, "NOT_AUTHORIZED")
		return true
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !lifecycleID(keys[0]) || len(keys[0]) < 8 || r.URL.RawQuery != "" || r.Header.Get("Content-Encoding") != "" {
		writePluginProblem(w, 400, "INVALID_REQUEST")
		return true
	}
	var params json.RawMessage
	if r.Method == http.MethodDelete && r.ContentLength == 0 {
		params = json.RawMessage(`{}`)
	} else {
		media, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if len(r.Header.Values("Content-Type")) != 1 || mediaErr != nil || media != "application/json" {
			writePluginProblem(w, 400, "INVALID_REQUEST")
			return true
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
		if err != nil || bytes.Contains(raw, []byte("-----BEGIN")) {
			writePluginProblem(w, 400, "INVALID_REQUEST")
			return true
		}
		var object map[string]json.RawMessage
		budget := 4096
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if checkXrayJSON(decoder, 0, &budget) != nil {
			writePluginProblem(w, 400, "INVALID_REQUEST")
			return true
		}
		if _, err := decoder.Token(); err != io.EOF {
			writePluginProblem(w, 400, "INVALID_REQUEST")
			return true
		}
		if json.Unmarshal(raw, &object) != nil || object == nil {
			writePluginProblem(w, 400, "INVALID_REQUEST")
			return true
		}
		// Secret material is held by the central credential service, referenced
		// by ID. Reject generic secret injection before any task is submitted.
		for key := range object {
			switch strings.ToLower(key) {
			case "secret", "token", "password", "privatekey", "command", "shell":
				writePluginProblem(w, 400, "INVALID_REQUEST")
				return true
			}
		}
		params, _ = json.Marshal(object)
	}
	input := XrayTaskRequest{Principal: principal, Installation: installation, Operation: operation, NodeID: r.PathValue("nodeId"), UserID: r.PathValue("userId"), IdempotencyKey: keys[0], Parameters: params}
	bound, _ := json.Marshal(input)
	sum := sha256.Sum256(bound)
	input.ParametersSHA256 = hex.EncodeToString(sum[:])
	if h.taskHost.Admit(r.Context(), cloneXrayTask(input)) != nil || r.Context().Err() != nil {
		writePluginProblem(w, 403, "NOT_AUTHORIZED")
		return true
	}
	id, err := h.taskHost.Submit(r.Context(), cloneXrayTask(input))
	fresh, stateErr := h.state(r.Context())
	if err != nil || stateErr != nil || fresh != installation || !lifecycleID(id) || h.taskHost.Admit(r.Context(), cloneXrayTask(input)) != nil || r.Context().Err() != nil {
		writePluginProblem(w, 503, "XRAY_TASK_OUTCOME_UNKNOWN")
		return true
	}
	writePluginJSON(w, 202, map[string]string{"taskId": id, "status": "queued"})
	return true
}

func checkXrayJSON(d *json.Decoder, depth int, budget *int) error {
	*budget--
	if depth > 16 || *budget < 0 {
		return ErrPackageRejected
	}
	token, err := d.Token()
	if err != nil {
		return ErrPackageRejected
	}
	if delim, ok := token.(json.Delim); ok {
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				keyToken, e := d.Token()
				key, ok := keyToken.(string)
				if e != nil || !ok {
					return ErrPackageRejected
				}
				normalized := strings.ToLower(key)
				if seen[normalized] {
					return ErrPackageRejected
				}
				seen[normalized] = true
				switch normalized {
				case "secret", "token", "password", "privatekey", "command", "shell":
					return ErrPackageRejected
				}
				if checkXrayJSON(d, depth+1, budget) != nil {
					return ErrPackageRejected
				}
			}
			if end, e := d.Token(); e != nil || end != json.Delim('}') {
				return ErrPackageRejected
			}
		case '[':
			for d.More() {
				if checkXrayJSON(d, depth+1, budget) != nil {
					return ErrPackageRejected
				}
			}
			if end, e := d.Token(); e != nil || end != json.Delim(']') {
				return ErrPackageRejected
			}
		default:
			return ErrPackageRejected
		}
	}
	return nil
}
