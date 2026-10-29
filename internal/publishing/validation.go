package publishing

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxPlanNameBytes  = 128
	maxSummaryBytes   = 512
	maxDomainBytes    = 253
	maxErrorCodeBytes = 64
)

var (
	secretAssignment    = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|api[_-]?key|private[_-]?key)\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;]+)`)
	authorizationHeader = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*(?:bearer\s+)?[^\s,;]+`)
	bearerToken         = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	userInfoURL         = regexp.MustCompile(`(?i)(https?://)[^/@\s]+@`)
	pemBlock            = regexp.MustCompile(`(?is)-----BEGIN [A-Z ]+-----.*?-----END [A-Z ]+-----`)
)

func normalizeCreateInput(input CreateInput) (CreateInput, error) {
	if !validID(input.ID) {
		return CreateInput{}, fmt.Errorf("%w: id", ErrInvalidPlan)
	}
	if !validText(input.Name, maxPlanNameBytes) {
		return CreateInput{}, fmt.Errorf("%w: name", ErrInvalidPlan)
	}
	domain, err := normalizeDomain(input.Domain)
	if err != nil {
		return CreateInput{}, err
	}
	input.Domain = domain
	if !validID(input.ServiceGroupID) {
		return CreateInput{}, fmt.Errorf("%w: service group id", ErrInvalidPlan)
	}
	if input.ExpectedGroupRevision == 0 {
		return CreateInput{}, fmt.Errorf("%w: expected group revision", ErrInvalidPlan)
	}
	if input.IdempotencyKey != "" && !validID(input.IdempotencyKey) {
		return CreateInput{}, fmt.Errorf("%w: idempotency key", ErrInvalidPlan)
	}
	return input, nil
}

func normalizeGroupRef(ref GroupRef, serviceGroupID string) (GroupRef, error) {
	if ref.Revision == 0 {
		return GroupRef{}, ErrGroupLookupFailed
	}
	if ref.ID != "" && ref.ID != serviceGroupID {
		return GroupRef{}, ErrGroupLookupFailed
	}
	seen := make(map[string]struct{}, len(ref.NodeIDs))
	nodes := make([]string, 0, len(ref.NodeIDs))
	for _, nodeID := range ref.NodeIDs {
		if !validID(nodeID) {
			return GroupRef{}, ErrGroupLookupFailed
		}
		if _, duplicate := seen[nodeID]; duplicate {
			continue
		}
		seen[nodeID] = struct{}{}
		nodes = append(nodes, nodeID)
	}
	sort.Strings(nodes)
	ref.ID = serviceGroupID
	ref.NodeIDs = nodes
	return ref, nil
}

func normalizeStageResult(result StageResult) (string, error) {
	summary := sanitizeMessage(result.Summary)
	if summary == "" {
		return "", ErrInvalidStageResult
	}
	return summary, nil
}

func normalizeStageKind(kind StageKind) (StageKind, error) {
	if !kind.Valid() {
		return "", fmt.Errorf("%w: stage", ErrInvalidStage)
	}
	return kind, nil
}

func normalizeDomain(raw string) (string, error) {
	if raw == "" || len(raw) > maxDomainBytes || strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("%w: domain", ErrInvalidPlan)
	}
	domain := strings.ToLower(raw)
	if strings.Contains(domain, "*") && !strings.HasPrefix(domain, "*.") {
		return "", fmt.Errorf("%w: wildcard domain", ErrInvalidPlan)
	}
	base := domain
	if strings.HasPrefix(domain, "*.") {
		base = domain[2:]
	}
	if err := validateDomainLabels(base); err != nil {
		return "", err
	}
	return domain, nil
}

func validateDomainLabels(domain string) error {
	if domain == "" || len(domain) > maxDomainBytes {
		return fmt.Errorf("%w: domain", ErrInvalidPlan)
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return fmt.Errorf("%w: domain needs at least two labels", ErrInvalidPlan)
	}
	for _, label := range labels {
		if !validHostLabel(label) {
			return fmt.Errorf("%w: domain label", ErrInvalidPlan)
		}
	}
	if allDigits(labels[len(labels)-1]) {
		return fmt.Errorf("%w: domain must not be an IP address", ErrInvalidPlan)
	}
	return nil
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

// sanitizeMessage produces bounded single-line secret-free text. It is used for
// result summaries and never for authority decisions.
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
	return truncateBytes(cleaned, maxSummaryBytes)
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

func validErrorCode(value string) bool {
	if value == "" || len(value) > maxErrorCodeBytes {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return true
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
