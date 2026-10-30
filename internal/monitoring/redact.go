package monitoring

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	probeSecretAssignment = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key|private[_-]?key|authorization)\b(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	probeSecretFlag       = regexp.MustCompile(`(?i)(--?(?:password|passwd|token|secret|api[_-]?key))\s+\S+`)
	probeBearerToken      = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	probeURLUserInfo      = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s@]+@`)
)

const maxProbeErrorBytes = 512

// sanitizeProbeText removes URL userinfo, common secret shapes and control
// characters before an error is persisted or placed in an alert.
func sanitizeProbeText(value string) string {
	out := probeURLUserInfo.ReplaceAllString(value, "${1}[REDACTED]@")
	out = probeBearerToken.ReplaceAllString(out, "Bearer [REDACTED]")
	out = probeSecretAssignment.ReplaceAllString(out, "${1}${2}[REDACTED]")
	out = probeSecretFlag.ReplaceAllString(out, "${1} [REDACTED]")
	out = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return r
	}, out)
	out = strings.Join(strings.Fields(out), " ")
	if out == "" {
		return "probe failed"
	}
	if len(out) > maxProbeErrorBytes {
		truncated := out[:maxProbeErrorBytes]
		for !utf8.ValidString(truncated) && len(truncated) > 0 {
			truncated = truncated[:len(truncated)-1]
		}
		out = truncated
	}
	return out
}
