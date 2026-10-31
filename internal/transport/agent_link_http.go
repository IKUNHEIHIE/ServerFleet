package transport

import (
	"crypto/tls"
	"net/http"
	"strings"
)

// AgentLinkHTTP is the Agent-initiated WSS endpoint. It requires an already
// authenticated mTLS connection whose leaf maps to an enabled enrollment
// identity; ControllerLinks repeats that verification at handshake/dispatch.
type AgentLinkHTTP struct {
	links      *ControllerLinks
	authority  string
	serverName string
	cfg        WebSocketConfig
}

func NewAgentLinkHTTP(links *ControllerLinks, authority, serverName string, cfg WebSocketConfig) (*AgentLinkHTTP, error) {
	if links == nil || authority == "" || serverName == "" || strings.ContainsAny(authority+serverName, "\r\n\x00") || !cfg.valid() {
		return nil, ErrLinkRejected
	}
	return &AgentLinkHTTP{links: links, authority: authority, serverName: serverName, cfg: cfg}, nil
}

func (h *AgentLinkHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.EscapedPath() != "/api/v1/agent/link" {
		writeTransportProblem(w, http.StatusNotFound, "NOT_FOUND")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeTransportProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	if r.TLS == nil || !r.TLS.HandshakeComplete || r.TLS.Version < tls.VersionTLS13 || r.TLS.ServerName != h.serverName || r.Host != h.authority || len(r.TLS.PeerCertificates) == 0 || len(r.TLS.VerifiedChains) == 0 {
		writeTransportProblem(w, http.StatusForbidden, "AGENT_LINK_SOURCE_REJECTED")
		return
	}
	for _, name := range []string{"Origin", "Cookie", "Authorization", "Proxy-Authorization", "Referer", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "X-CSRF-Token"} {
		if len(r.Header.Values(name)) != 0 {
			writeTransportProblem(w, http.StatusForbidden, "AGENT_LINK_SOURCE_REJECTED")
			return
		}
	}
	if r.URL.RawQuery != "" {
		writeTransportProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	socket, connectionID, hijacked, err := AcceptWebSocket(w, r, h.cfg)
	if err != nil {
		if !hijacked {
			writeTransportProblem(w, http.StatusBadRequest, "UPGRADE_REJECTED")
		}
		return
	}
	_ = h.links.Serve(r.Context(), socket, *r.TLS, connectionID)
}
