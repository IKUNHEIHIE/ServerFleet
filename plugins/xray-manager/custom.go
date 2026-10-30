package xraymanager

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CustomPolicy is assembled from current controlled preflight/file grants.
// Never deserialize it from the browser's config submission. OS sandbox/file
// confinement and current sole-task/plugin admission are additional boundaries.
type CustomPolicy struct {
	InstanceID, TargetVersion           string
	AllowedReadFiles, AllowedWriteFiles []string
}
type CustomReview struct {
	SHA256       string   `json:"sha256"`
	WarningCodes []string `json:"warningCodes"`
	InboundCount int      `json:"inboundCount"`
}

// A separate bounded decoder rejects duplicate/case-alias JSON keys that the
// target's struct decoder could otherwise interpret differently. This tree is
// ONLY for review; deployment must use the original digest-bound bytes.
func readCustomValue(d *json.Decoder, depth int, remaining *int) (any, error) {
	*remaining--
	if depth > 32 || *remaining < 0 {
		return nil, ErrConfigurationRejected
	}
	t, err := d.Token()
	if err != nil {
		return nil, ErrConfigurationRejected
	}
	if delimiter, ok := t.(json.Delim); ok {
		switch delimiter {
		case '{':
			m := map[string]any{}
			for d.More() {
				key, err := d.Token()
				k, ok := key.(string)
				if err != nil || !ok || k == "" || len(k) > 256 {
					return nil, ErrConfigurationRejected
				}
				k = strings.ToLower(k)
				if _, exists := m[k]; exists {
					return nil, ErrConfigurationRejected
				}
				v, err := readCustomValue(d, depth+1, remaining)
				if err != nil {
					return nil, err
				}
				m[k] = v
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, ErrConfigurationRejected
			}
			return m, nil
		case '[':
			list := []any{}
			for d.More() {
				v, err := readCustomValue(d, depth+1, remaining)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, ErrConfigurationRejected
			}
			return list, nil
		default:
			return nil, ErrConfigurationRejected
		}
	}
	return t, nil
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func array(v any) []any           { a, _ := v.([]any); return a }
func textValue(v any) string      { s, _ := v.(string); return s }
func customPort(v any) string {
	var s string
	switch n := v.(type) {
	case json.Number:
		s = string(n)
	case string:
		s = n
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != s {
		return ""
	}
	return s
}
func loopbackListener(v any) string {
	host, port, err := net.SplitHostPort(textValue(v))
	addr, e := netip.ParseAddr(host)
	if err != nil || e != nil || !addr.IsLoopback() || customPort(port) == "" {
		return ""
	}
	return port
}
func containsString(v any, expected string) bool {
	switch x := v.(type) {
	case string:
		return x == expected
	case []any:
		for _, child := range x {
			if containsString(child, expected) {
				return true
			}
		}
	case map[string]any:
		for _, child := range x {
			if containsString(child, expected) {
				return true
			}
		}
	}
	return false
}

func reviewCustomAPI(root map[string]any, inbounds []any, outbounds []any) error {
	api, exists := root["api"]
	if !exists {
		return nil
	}
	a := object(api)
	tag := textValue(a["tag"])
	if !safeConfigID(tag) || len(array(a["services"])) == 0 {
		return ErrConfigurationRejected
	}
	for key := range a {
		if key != "tag" && key != "listen" && key != "services" {
			return ErrConfigurationRejected
		}
	}
	for _, service := range array(a["services"]) {
		if service != "StatsService" && service != "HandlerService" {
			return ErrConfigurationRejected
		}
	}
	routing := object(root["routing"])
	if _, balancers := routing["balancers"]; balancers {
		return ErrConfigurationRejected
	}
	rules := array(routing["rules"])
	port := loopbackListener(a["listen"])
	localTag := ""
	start := 0
	if _, direct := a["listen"]; !direct {
		if len(rules) == 0 {
			return ErrConfigurationRejected
		}
		first := object(rules[0])
		localTags := array(first["inboundtag"])
		if len(first) != 3 || first["type"] != "field" || first["outboundtag"] != tag || len(localTags) != 1 {
			return ErrConfigurationRejected
		}
		localTag = textValue(localTags[0])
		for _, item := range inbounds {
			ib := object(item)
			if ib["tag"] == localTag {
				addr, err := netip.ParseAddr(textValue(ib["listen"]))
				settings := object(ib["settings"])
				if err != nil || !addr.IsLoopback() || ib["protocol"] != "dokodemo-door" || len(ib) != 5 || len(settings) != 1 || settings["address"] != "127.0.0.1" {
					return ErrConfigurationRejected
				}
				port = customPort(ib["port"])
			}
		}
		start = 1
	}
	if port == "" || len(rules) <= start {
		return ErrConfigurationRejected
	}
	// Before any custom routing, every non-management request to the API port
	// must be blocked, including DNS aliases resolving back to the local API.
	deny := object(rules[start])
	denyTag := textValue(deny["outboundtag"])
	if len(deny) != 3 || deny["type"] != "field" || customPort(deny["port"]) != port || denyTag == "" || denyTag == tag {
		return ErrConfigurationRejected
	}
	blocked := false
	for _, item := range outbounds {
		ob := object(item)
		if ob["protocol"] == "loopback" || containsString(ob, tag) || localTag != "" && containsString(ob, localTag) {
			return ErrConfigurationRejected
		}
		if ob["tag"] == denyTag && ob["protocol"] == "blackhole" {
			blocked = true
		}
	}
	if !blocked {
		return ErrConfigurationRejected
	}
	for i, rule := range rules {
		if i < start {
			continue
		}
		if containsString(rule, tag) || localTag != "" && containsString(rule, localTag) {
			return ErrConfigurationRejected
		}
	}
	// No additional module may alias the private API/inbound via a tag.
	for key, value := range root {
		if key != "api" && key != "routing" && key != "inbounds" && key != "outbounds" && (containsString(value, tag) || localTag != "" && containsString(value, localTag)) {
			return ErrConfigurationRejected
		}
	}
	for _, item := range inbounds {
		ib := object(item)
		if ib["tag"] != localTag && (containsString(ib, tag) || localTag != "" && containsString(ib, localTag)) {
			return ErrConfigurationRejected
		}
	}
	return nil
}

func customFileAllowed(file, instance string, allowed []string) bool {
	prefix := "/var/lib/serverfleet/xray/" + instance + "/"
	return strings.HasPrefix(file, prefix) && path.Clean(file) == file && !strings.ContainsAny(file, "\\\x00\r\n$") && slices.Contains(allowed, file)
}
func reviewCustomTree(v any, policy CustomPolicy, warnings map[string]bool) error {
	switch x := v.(type) {
	case []any:
		for _, child := range x {
			if err := reviewCustomTree(child, policy, warnings); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, child := range x {
			value, isString := child.(string)
			if key == "exec" || key == "command" || key == "script" || key == "include" || key == "confdir" {
				return ErrConfigurationRejected
			}
			if strings.Contains(key, "file") || key == "access" || key == "error" {
				allowed := policy.AllowedReadFiles
				noFile := isString && value == ""
				if key == "access" || key == "error" {
					allowed = policy.AllowedWriteFiles
					noFile = noFile || value == "none"
				}
				if !isString || !noFile && !customFileAllowed(value, policy.InstanceID, allowed) {
					return ErrConfigurationRejected
				}
			}
			if isString && (strings.HasPrefix(value, "ext:") || strings.HasPrefix(value, "ext-ip:") || strings.HasPrefix(value, "ext-domain:")) {
				return ErrConfigurationRejected
			}
			if key == "listen" && isString && (strings.HasPrefix(value, "/") || strings.HasPrefix(value, "@")) {
				return ErrConfigurationRejected
			}
			if key == "allowinsecure" && child == true {
				warnings["TLS_VERIFICATION_DISABLED"] = true
			}
			if key == "realitysettings" {
				warnings["REALITY_FALLBACK_REQUIRES_REVIEW"] = true
			}
			if key == "protocol" && value == "tun" {
				warnings["SYSTEM_NETWORK_CHANGE_REQUIRES_REVIEW"] = true
			}
			if key == "loglevel" && value == "debug" {
				warnings["DEBUG_LOG_MAY_CONTAIN_SENSITIVE_DATA"] = true
			}
			if err := reviewCustomTree(child, policy, warnings); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReviewCustomJSON preserves raw configuration bytes and returns NO raw values.
// This minimum safety review is not target-version validation or permission to
// run. The sole task must bind this digest, explicit warning consent, current
// actor/node/plugin/revision and an actual target-version `xray run -test` stage.
func ReviewCustomJSON(raw []byte, policy CustomPolicy) (CustomReview, error) {
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) || !safeConfigID(policy.InstanceID) || policy.TargetVersion != TestedXrayVersion || len(policy.AllowedReadFiles)+len(policy.AllowedWriteFiles) > 256 {
		return CustomReview{}, ErrConfigurationRejected
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	remaining := 100000
	v, err := readCustomValue(d, 0, &remaining)
	root := object(v)
	if err != nil || root == nil {
		return CustomReview{}, ErrConfigurationRejected
	}
	if _, err = d.Token(); err != io.EOF {
		return CustomReview{}, ErrConfigurationRejected
	}
	for key := range root {
		if !slices.Contains([]string{"log", "routing", "dns", "inbounds", "outbounds", "policy", "api", "metrics", "stats", "fakedns", "observatory", "burstobservatory", "version"}, key) {
			return CustomReview{}, ErrConfigurationRejected
		}
	}
	inbounds, outbounds := array(root["inbounds"]), array(root["outbounds"])
	if len(inbounds) == 0 || len(inbounds) > 128 || len(outbounds) == 0 || len(outbounds) > 128 {
		return CustomReview{}, ErrConfigurationRejected
	}
	for _, list := range [][]any{inbounds, outbounds} {
		tags := map[string]bool{}
		for _, item := range list {
			o := object(item)
			tag := textValue(o["tag"])
			if o == nil || !safeConfigID(tag) || tags[tag] || textValue(o["protocol"]) == "" {
				return CustomReview{}, ErrConfigurationRejected
			}
			tags[tag] = true
		}
	}
	if _, exists := root["metrics"]; exists {
		// A separate metrics listener could also be reached through proxy
		// clients. It needs an additional reviewed routing/isolation profile.
		return CustomReview{}, ErrConfigurationRejected
	}
	if reviewCustomAPI(root, inbounds, outbounds) != nil {
		return CustomReview{}, ErrConfigurationRejected
	}
	warnings := map[string]bool{"CUSTOM_JSON_ADMIN_REVIEW_REQUIRED": true, "TARGET_VERSION_TEST_REQUIRED": true, "TRAFFIC_AVAILABILITY_NOT_VERIFIED": true}
	if reviewCustomTree(root, policy, warnings) != nil {
		return CustomReview{}, ErrConfigurationRejected
	}
	result := CustomReview{InboundCount: len(inbounds)}
	hash := sha256.Sum256(raw)
	result.SHA256 = hex.EncodeToString(hash[:])
	for warning := range warnings {
		result.WarningCodes = append(result.WarningCodes, warning)
	}
	slices.Sort(result.WarningCodes)
	return result, nil
}
