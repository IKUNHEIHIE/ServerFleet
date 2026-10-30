package gateway

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxGatewayNameBytes = 128
	maxRegionBytes      = 64
	maxVersionBytes     = 64
	maxErrorBytes       = 512
	maxSteps            = 64
	maxStepNameBytes    = 64
	maxStepMessageBytes = 512
	maxDomainBytes      = 253
	maxUpstreamBytes    = 255
)

var (
	secretAssignment    = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|api[_-]?key|private[_-]?key)\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;]+)`)
	authorizationHeader = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*(?:bearer\s+)?[^\s,;]+`)
	bearerToken         = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	userInfoURL         = regexp.MustCompile(`(?i)(https?://)[^/@\s]+@`)
	pemBlock            = regexp.MustCompile(`(?is)-----BEGIN [A-Z ]+-----.*?-----END [A-Z ]+-----`)
)

func normalizeCreateGatewayInput(input CreateGatewayInput) (CreateGatewayInput, error) {
	if !validID(input.ID) {
		return CreateGatewayInput{}, fmt.Errorf("%w: id", ErrInvalidGateway)
	}
	if !validID(input.NodeID) {
		return CreateGatewayInput{}, fmt.Errorf("%w: node id", ErrInvalidGateway)
	}
	normalized, err := normalizeGatewayText(input.Name, input.Region, input.CountryCode)
	if err != nil {
		return CreateGatewayInput{}, err
	}
	input.Name, input.Region, input.CountryCode = normalized.name, normalized.region, normalized.country
	return input, nil
}

func normalizeUpdateGatewayInput(input UpdateGatewayInput) (UpdateGatewayInput, error) {
	normalized, err := normalizeGatewayText(input.Name, input.Region, input.CountryCode)
	if err != nil {
		return UpdateGatewayInput{}, err
	}
	input.Name, input.Region, input.CountryCode = normalized.name, normalized.region, normalized.country
	return input, nil
}

type gatewayText struct {
	name    string
	region  string
	country string
}

func normalizeGatewayText(name, region, countryCode string) (gatewayText, error) {
	if !validText(name, maxGatewayNameBytes) {
		return gatewayText{}, fmt.Errorf("%w: name", ErrInvalidGateway)
	}
	if region != "" && !validText(region, maxRegionBytes) {
		return gatewayText{}, fmt.Errorf("%w: region", ErrInvalidGateway)
	}
	country, err := normalizeCountryCode(countryCode)
	if err != nil {
		return gatewayText{}, err
	}
	return gatewayText{name: name, region: region, country: country}, nil
}

func normalizeCountryCode(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) != 2 {
		return "", fmt.Errorf("%w: country code", ErrInvalidGateway)
	}
	upper := strings.ToUpper(value)
	for _, r := range upper {
		if r < 'A' || r > 'Z' {
			return "", fmt.Errorf("%w: country code", ErrInvalidGateway)
		}
	}
	return upper, nil
}

func normalizeCheckReport(report CheckReport) (CheckReport, error) {
	if report.ChecksPassed && !report.NginxInstalled {
		return CheckReport{}, fmt.Errorf("%w: checks passed without an installed nginx", ErrInvalidGateway)
	}
	if report.NginxVersion != "" && !validText(report.NginxVersion, maxVersionBytes) {
		return CheckReport{}, fmt.Errorf("%w: nginx version", ErrInvalidGateway)
	}
	report.Error = sanitizeMessage(report.Error)
	return report, nil
}

func normalizeCreateRouteInput(input CreateRouteInput) (CreateRouteInput, error) {
	if !validID(input.ID) {
		return CreateRouteInput{}, fmt.Errorf("%w: id", ErrInvalidRoute)
	}
	input.ID = strings.TrimSpace(input.ID)
	if err := normalizeRouteFields(&input.Domain, &input.Upstream, &input.ServiceGroupID); err != nil {
		return CreateRouteInput{}, err
	}
	return input, nil
}

func normalizeUpdateRouteInput(input UpdateRouteInput) (UpdateRouteInput, error) {
	if err := normalizeRouteFields(&input.Domain, &input.Upstream, &input.ServiceGroupID); err != nil {
		return UpdateRouteInput{}, err
	}
	return input, nil
}

func normalizeRouteFields(domain, upstream, serviceGroupID *string) error {
	normalizedDomain, err := normalizeDomain(*domain)
	if err != nil {
		return err
	}
	*domain = normalizedDomain
	if *upstream == "" && *serviceGroupID == "" {
		return fmt.Errorf("%w: exactly one of upstream or service group is required", ErrInvalidRoute)
	}
	if *upstream != "" && *serviceGroupID != "" {
		return fmt.Errorf("%w: upstream and service group are mutually exclusive", ErrInvalidRoute)
	}
	if *upstream != "" {
		normalizedUpstream, err := normalizeUpstream(*upstream)
		if err != nil {
			return err
		}
		*upstream = normalizedUpstream
		*serviceGroupID = ""
		return nil
	}
	if !validID(*serviceGroupID) {
		return fmt.Errorf("%w: service group id", ErrInvalidRoute)
	}
	*upstream = ""
	return nil
}

func normalizeProvisionInput(input ProvisionInput) (ProvisionInput, error) {
	if input.ApprovalRef != "" && !validToken(input.ApprovalRef, 128) {
		return ProvisionInput{}, fmt.Errorf("%w: approval reference", ErrInvalidProvision)
	}
	if input.TakeoverApproved && input.ApprovalRef == "" {
		return ProvisionInput{}, ErrTakeoverApprovalRequired
	}
	if input.ExistingSite && (!input.TakeoverApproved || input.ApprovalRef == "") {
		return ProvisionInput{}, ErrTakeoverApprovalRequired
	}
	return input, nil
}

func normalizeSteps(steps []ProvisionStep) ([]ProvisionStep, error) {
	if len(steps) > maxSteps {
		return nil, fmt.Errorf("%w: too many steps", ErrInvalidProvision)
	}
	out := make([]ProvisionStep, 0, len(steps))
	for _, step := range steps {
		if !validID(step.Name) || len(step.Name) > maxStepNameBytes {
			return nil, fmt.Errorf("%w: step name", ErrInvalidProvision)
		}
		if !step.Status.Valid() {
			return nil, fmt.Errorf("%w: step status", ErrInvalidProvision)
		}
		out = append(out, ProvisionStep{
			Name:    step.Name,
			Status:  step.Status,
			Message: sanitizeMessage(step.Message),
		})
	}
	return out, nil
}

func validateStoredSteps(steps []ProvisionStep) error {
	for _, step := range steps {
		if !validID(step.Name) || !step.Status.Valid() || len(step.Message) > maxStepMessageBytes {
			return fmt.Errorf("%w: stored step", ErrInvalidProvision)
		}
		if step.Message != sanitizeMessage(step.Message) {
			return fmt.Errorf("%w: stored step is not redacted", ErrInvalidProvision)
		}
	}
	return nil
}

func normalizeDomain(raw string) (string, error) {
	if raw == "" || len(raw) > maxDomainBytes || strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("%w: domain", ErrInvalidRoute)
	}
	domain := strings.ToLower(raw)
	if strings.Contains(domain, "*") && !strings.HasPrefix(domain, "*.") {
		return "", fmt.Errorf("%w: wildcard domain", ErrInvalidRoute)
	}
	if strings.HasPrefix(domain, "*.") {
		base := domain[2:]
		if err := validateDomainLabels(base, true); err != nil {
			return "", err
		}
		return domain, nil
	}
	if err := validateDomainLabels(domain, false); err != nil {
		return "", err
	}
	return domain, nil
}

func validateDomainLabels(domain string, wildcard bool) error {
	if domain == "" || len(domain) > maxDomainBytes {
		return fmt.Errorf("%w: domain", ErrInvalidRoute)
	}
	labels := strings.Split(domain, ".")
	minimum := 2
	if wildcard {
		minimum = 2
	}
	if len(labels) < minimum {
		return fmt.Errorf("%w: domain needs at least two labels", ErrInvalidRoute)
	}
	for _, label := range labels {
		if !validHostLabel(label) {
			return fmt.Errorf("%w: domain label", ErrInvalidRoute)
		}
	}
	last := labels[len(labels)-1]
	if allDigits(last) {
		return fmt.Errorf("%w: domain must not be an IP address", ErrInvalidRoute)
	}
	return nil
}

func normalizeUpstream(raw string) (string, error) {
	if raw == "" || len(raw) > maxUpstreamBytes || strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("%w: upstream", ErrInvalidRoute)
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return "", fmt.Errorf("%w: upstream", ErrInvalidRoute)
	}
	host, portText, err := net.SplitHostPort(raw)
	if err != nil || host == "" || portText == "" {
		return "", fmt.Errorf("%w: upstream must be host:port", ErrInvalidRoute)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("%w: upstream port", ErrInvalidRoute)
	}
	if ip := net.ParseIP(host); ip != nil {
		return net.JoinHostPort(host, strconv.Itoa(port)), nil
	}
	host = strings.ToLower(host)
	if len(host) > maxDomainBytes || strings.Contains(host, "%") {
		return "", fmt.Errorf("%w: upstream host", ErrInvalidRoute)
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !validHostLabel(label) {
			return "", fmt.Errorf("%w: upstream host", ErrInvalidRoute)
		}
	}
	if allDigits(labels[len(labels)-1]) {
		return "", fmt.Errorf("%w: upstream host", ErrInvalidRoute)
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func validHostLabel(label string) bool {
	if label == "" || len(label) > 63 {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for index := 0; index < len(label); index++ {
		r := label[index]
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sanitizeMessage produces bounded, single-line, secret-free text for persisted
// error and step fields.
func sanitizeMessage(raw string) string {
	if raw == "" {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return r
	}, raw)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	cleaned = pemBlock.ReplaceAllString(cleaned, "[redacted]")
	cleaned = authorizationHeader.ReplaceAllString(cleaned, "authorization=[redacted]")
	cleaned = secretAssignment.ReplaceAllString(cleaned, "$1=[redacted]")
	cleaned = bearerToken.ReplaceAllString(cleaned, "Bearer [redacted]")
	cleaned = userInfoURL.ReplaceAllString(cleaned, "$1[redacted]@")
	cleaned = strings.TrimSpace(cleaned)
	return truncateBytes(cleaned, maxErrorBytes)
}

func truncateBytes(value string, max int) string {
	if len(value) <= max {
		return value
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

func validText(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
}

func validToken(value string, max int) bool {
	return validText(value, max) && !strings.ContainsFunc(value, unicode.IsSpace)
}

func validID(value string) bool {
	return validToken(value, 128) && !strings.ContainsAny(value, "*?[]/\\")
}
