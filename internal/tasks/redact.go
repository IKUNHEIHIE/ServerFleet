package tasks

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	secretAssignment = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key|private[_-]?key|authorization)\b(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	secretFlag       = regexp.MustCompile(`(?i)(--?(?:password|passwd|token|secret|api[_-]?key))\s+\S+`)
	bearerToken      = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
)

// Redact removes common secret shapes from task logs before persistence. It is a
// defense-in-depth filter, not a guarantee: callers must still avoid logging
// credentials or private key material.
func Redact(message string) string {
	redacted := bearerToken.ReplaceAllString(message, "Bearer [REDACTED]")
	redacted = secretAssignment.ReplaceAllString(redacted, "${1}${2}[REDACTED]")
	redacted = secretFlag.ReplaceAllString(redacted, "${1} [REDACTED]")
	return stripControl(redacted)
}

func stripControl(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		switch {
		case r == '\n' || r == '\t':
			builder.WriteRune(r)
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp):
			builder.WriteRune(' ')
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
