package notifications

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	secretAssignment = regexp.MustCompile(`(?i)\b(password|passwd|secret|appsecret|token|api[_-]?key|private[_-]?key|authorization)\b(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	secretFlag       = regexp.MustCompile(`(?i)(--?(?:password|passwd|token|secret|api[_-]?key|appsecret))\s+\S+`)
	bearerToken      = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	urlUserInfo      = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s@]+@`)
	providerCodeSafe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// redact removes common credential shapes and URL userinfo before text is stored,
// audited or handed to a provider. It is defense in depth, not a substitute for
// never placing a credential in a message.
func redact(value string) string {
	out := urlUserInfo.ReplaceAllString(value, "${1}[REDACTED]@")
	out = bearerToken.ReplaceAllString(out, "Bearer [REDACTED]")
	out = secretAssignment.ReplaceAllString(out, "${1}${2}[REDACTED]")
	out = secretFlag.ReplaceAllString(out, "${1} [REDACTED]")
	return stripControl(out)
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

// sanitizeProviderCode keeps only a short opaque code. Anything resembling a URL
// or free-form upstream text becomes "provider_error".
func sanitizeProviderCode(code string) string {
	if code == "" {
		return ""
	}
	if !providerCodeSafe.MatchString(code) {
		return "provider_error"
	}
	return code
}

func redactedMessage(message Message) Message {
	return Message{Subject: redact(message.Subject), Body: redact(message.Body)}
}
