package monitoring

import (
	"context"
	"serverfleet.local/serverfleet/internal/netprobe"
)

type NetProber struct{ inner *netprobe.NetProber }
type NetProberConfig = netprobe.NetProberConfig
type HostResolver = netprobe.HostResolver
type ProbeError = netprobe.ProbeError

func NewNetProber(c NetProberConfig) (*NetProber, error) {
	n, e := netprobe.NewNetProber(c)
	if e != nil {
		return nil, e
	}
	return &NetProber{n}, nil
}
func resultFromNetwork(r netprobe.ProbeResult, e error) (ProbeResult, error) {
	return ProbeResult{Healthy: r.Healthy, StatusCode: r.StatusCode, Latency: r.Latency, Reason: r.Reason}, e
}
func (n *NetProber) HTTP(c context.Context, s TargetSpec) (ProbeResult, error) {
	return resultFromNetwork(n.inner.HTTP(c, s))
}
func (n *NetProber) TCP(c context.Context, s TargetSpec) (ProbeResult, error) {
	return resultFromNetwork(n.inner.TCP(c, s))
}
func (n *NetProber) TLS(c context.Context, s TargetSpec) (ProbeResult, error) {
	return resultFromNetwork(n.inner.TLS(c, s))
}

const (
	ProbeCodeInvalidTarget   = netprobe.ProbeCodeInvalidTarget
	ProbeCodeTargetForbidden = netprobe.ProbeCodeTargetForbidden
	ProbeCodeDNSFailure      = netprobe.ProbeCodeDNSFailure
	ProbeCodeDialFailed      = netprobe.ProbeCodeDialFailed
	ProbeCodeTLSFailed       = netprobe.ProbeCodeTLSFailed
	ProbeCodeRequestFailed   = netprobe.ProbeCodeRequestFailed
	ProbeCodeTimeout         = netprobe.ProbeCodeTimeout
)
