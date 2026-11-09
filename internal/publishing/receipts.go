package publishing

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"path"
	"strings"
	"unicode"
)

// GatewayReceipt owns the bounded non-secret resource identity needed for real
// inverse compensation. Neither arbitrary provider text nor credential-shaped
// JSON is entitled to bypass human-message redaction.
type GatewayReceipt struct {
	RouteID            string `json:"routeId"`
	GatewayID          string `json:"gatewayId"`
	NodeID             string `json:"nodeId"`
	SitePath           string `json:"sitePath"`
	ConfigSHA256       string `json:"configSha256"`
	TaskID             string `json:"taskId"`
	RouteRevision      uint64 `json:"routeRevision"`
	RouteCreated       bool   `json:"routeCreated"`
	PlanID             string `json:"planId"`
	PlanTargetRevision uint64 `json:"planTargetRevision"`
	ProbeToken         string `json:"probeToken,omitempty"`
	HTTPStatus         int    `json:"httpStatus,omitempty"`
	ResponseSHA256     string `json:"responseSha256,omitempty"`
	ResponseBytes      int64  `json:"responseBytes,omitempty"`
}

func receiptDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}
func strictReceiptDecode(raw string, value any) bool {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return false
	}
	var trailing any
	return decoder.Decode(&trailing) == io.EOF
}
func managedGatewayReceipt(raw string) (string, bool) {
	if len(raw) == 0 || len(raw) > 4096 {
		return "", false
	}
	var r GatewayReceipt
	if !strictReceiptDecode(raw, &r) || !validID(r.GatewayID) || !validID(r.NodeID) || !validID(r.PlanID) || r.PlanTargetRevision == 0 ||
		r.RouteID != "" && !validID(r.RouteID) || r.TaskID != "" && !validID(r.TaskID) ||
		r.RouteCreated && (r.RouteID == "" || r.RouteRevision == 0) || r.ResponseBytes < 0 || r.ResponseBytes > 64<<10 {
		return "", false
	}
	if r.SitePath != "" {
		if !path.IsAbs(r.SitePath) || path.Clean(r.SitePath) != r.SitePath || len(r.SitePath) > 1024 || strings.ContainsFunc(r.SitePath, unicode.IsControl) || !receiptDigest(r.ConfigSHA256) {
			return "", false
		}
	} else if r.ConfigSHA256 != "" {
		return "", false
	}
	if r.ProbeToken != "" && !receiptDigest(r.ProbeToken) {
		return "", false
	}
	if r.HTTPStatus != 0 && (r.HTTPStatus < 200 || r.HTTPStatus >= 300 || !receiptDigest(r.ResponseSHA256) || r.ProbeToken == "" || r.SitePath == "" || r.TaskID == "") {
		return "", false
	}
	if r.ResponseSHA256 != "" && !receiptDigest(r.ResponseSHA256) {
		return "", false
	}
	encoded, err := json.Marshal(r)
	return string(encoded), err == nil
}

// DNSReceipt contains no provider credentials and never authorizes a deletion;
// the owner batch and exact task/plan evidence must still be reloaded.
type DNSReceipt struct {
	DNSBatchID string `json:"dnsBatchId"`
	TaskID     string `json:"taskId"`
}

func normalizeManagedReceipt(raw string) (string, bool) {
	var dnsReceipt DNSReceipt
	if strictReceiptDecode(raw, &dnsReceipt) && receiptDigest(dnsReceipt.DNSBatchID) && validID(dnsReceipt.TaskID) {
		encoded, err := json.Marshal(dnsReceipt)
		return string(encoded), err == nil
	}

	if canonical, ok := managedGatewayReceipt(raw); ok {
		return canonical, true
	}
	var batch gatewayBatchReceipt
	if !strictReceiptDecode(raw, &batch) || batch.Version != 1 || len(batch.Items) == 0 || len(batch.Items) > 8 {
		return "", false
	}
	for i, item := range batch.Items {
		if canonical, ok := managedGatewayReceipt(item); ok {
			batch.Items[i] = canonical
		} else {
			batch.Items[i] = sanitizeMessage(item)
		}
		if batch.Items[i] == "" {
			return "", false
		}
	}
	encoded, err := json.Marshal(batch)
	if err != nil || len(encoded) > maxReceiptBytes || bytes.Contains(encoded, []byte{0}) {
		return "", false
	}
	return string(encoded), true
}
