package netprobe

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Kind string

const (
	KindHTTP Kind = "http"
	KindTCP  Kind = "tcp"
	KindTLS  Kind = "tls"
)

func (k Kind) Valid() bool {
	return k == KindHTTP || k == KindTCP || k == KindTLS
}

type ProbeResult struct {
	Healthy    bool
	StatusCode *int
	Latency    *time.Duration
	Reason     string
}

// TargetSpec is handed to the injected Prober. Timeout is derived from the stored
// monitor timeout.
type TargetSpec struct {
	Kind    Kind
	Target  string
	Timeout time.Duration
}

var (
	ErrInvalidTarget       = errors.New("monitoring: invalid target")
	ErrTargetForbidden     = errors.New("monitoring: target is forbidden")
	ErrInvalidAllowedHosts = errors.New("monitoring: invalid allowed host entry")
)

func validText(value string, max int) bool {
	return value != "" && len(value) <= max && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) })
}
