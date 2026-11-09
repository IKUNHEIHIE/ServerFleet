package updates

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// PinnedHTTPSClient has no environment proxy, DNS fallback, redirect or caller
// selected origin. Addresses and trust are exclusively provisioned at startup.
func PinnedHTTPSClient(feed HTTPReleaseFeed, certificate *tls.Certificate) (*http.Client, error) {
	u, e := url.Parse(feed.Origin)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || u.RawPath != "" || (u.Path != "" && u.Path != "/") || len(feed.Addresses) == 0 || len(feed.Addresses) > 8 {
		return nil, ErrArtifactRejected
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	number, e := strconv.Atoi(port)
	if e != nil || number < 1 || number > 65535 {
		return nil, ErrArtifactRejected
	}
	addresses := append([]string{}, feed.Addresses...)
	seen := map[string]bool{}
	for _, s := range addresses {
		ip := net.ParseIP(s)
		if ip == nil || ip.String() != s || ip.IsUnspecified() || ip.IsMulticast() || seen[s] {
			return nil, ErrArtifactRejected
		}
		seen[s] = true
	}
	roots := x509.NewCertPool()
	if len(feed.RootPEM) == 0 || len(feed.RootPEM) > 128<<10 || !roots.AppendCertsFromPEM(feed.RootPEM) {
		return nil, ErrArtifactRejected
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: u.Hostname(), SessionTicketsDisabled: true}
	if certificate != nil {
		config.Certificates = []tls.Certificate{*certificate}
	}
	tr := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, MaxResponseHeaderBytes: 16 << 10, TLSClientConfig: config, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != net.JoinHostPort(u.Hostname(), port) {
			return nil, ErrArtifactRejected
		}
		var last error
		for _, ip := range addresses {
			conn, e := (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: -1}).DialContext(ctx, "tcp", net.JoinHostPort(ip, port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}}
	return &http.Client{Transport: tr, Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrArtifactRejected }}, nil
}
func AcquireResidentRelease(ctx context.Context, feed HTTPReleaseFeed, inbox, version, key string, policy ReleasePolicy, current func(context.Context) error) error {
	if ctx == nil || current == nil || !versionPattern.MatchString(version) || !healthDirectoryPrivate(inbox) {
		return ErrArtifactRejected
	}
	client, e := PinnedHTTPSClient(feed, nil)
	if e != nil {
		return e
	}
	defer client.CloseIdleConnections()
	root, e := os.OpenRoot(inbox)
	if e != nil {
		return e
	}
	defer root.Close()
	if _, e := root.Lstat(version); e == nil {
		return nil
	} // Stage still verifies every existing byte/signature.
	get := func(name string, max int64) ([]byte, error) {
		if current(ctx) != nil {
			return nil, ErrApplyUnknown
		}
		req, e := http.NewRequestWithContext(ctx, "GET", feed.Origin+"/releases/"+policy.Component+"/linux/"+policy.Architecture+"/"+version+"/"+name, nil)
		if e != nil {
			return nil, e
		}
		response, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		defer response.Body.Close()
		if response.StatusCode != 200 || response.ContentLength > max || response.Header.Get("Content-Encoding") != "" {
			return nil, ErrArtifactRejected
		}
		b, e := io.ReadAll(io.LimitReader(response.Body, max+1))
		if e != nil || int64(len(b)) > max || current(ctx) != nil {
			return nil, ErrApplyUnknown
		}
		return b, nil
	}
	raw, e := get("manifest.json", 16<<10)
	if e != nil {
		return e
	}
	sig, e := get("signature.bin", 64)
	if e != nil {
		return e
	}
	m, e := VerifyManifest(ctx, raw, sig, key, policy)
	if e != nil || m.Version != version {
		return ErrArtifactRejected
	}
	// The incomplete directory stays unknown if any network/write is ambiguous.
	if root.Mkdir(version, 0700) != nil || syncStagingDirectory(root, ".") != nil {
		return ErrUpdateBusy
	}
	put := func(name string, b []byte) error {
		f, e := root.OpenFile(version+"/"+name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		n, we := f.Write(b)
		sy := f.Sync()
		ce := f.Close()
		if we != nil || n != len(b) || sy != nil || ce != nil {
			return ErrApplyUnknown
		}
		return nil
	}
	if current(ctx) != nil {
		return ErrApplyUnknown
	}
	req, e := http.NewRequestWithContext(ctx, "GET", feed.Origin+"/releases/"+policy.Component+"/linux/"+policy.Architecture+"/"+version+"/artifact", nil)
	if e != nil {
		return e
	}
	response, e := client.Do(req)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Encoding") != "" || response.ContentLength > m.Bytes {
		return ErrArtifactRejected
	}
	f, e := root.OpenFile(version+"/artifact", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	reader := io.TeeReader(contextReader{ctx, response.Body}, f)
	_, verifyErr := VerifyRelease(ctx, raw, sig, key, reader, policy)
	sy := f.Sync()
	ce := f.Close()
	if verifyErr != nil || sy != nil || ce != nil || current(ctx) != nil {
		return ErrApplyUnknown
	}
	if put("signature.bin", sig) != nil || put("manifest.json", raw) != nil || syncStagingDirectory(root, version) != nil {
		return ErrApplyUnknown
	}
	return nil
}
