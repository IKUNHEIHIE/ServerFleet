package recovery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrRemoteUnknown = errors.New("recovery: remote result unknown; inspect before another write")
var ErrRemoteRejected = errors.New("recovery: remote object or destination rejected")

type BackupDestination struct {
	Kind               string `json:"kind"`
	Endpoint           string `json:"endpoint"`
	Bucket             string `json:"bucket,omitempty"`
	Region             string `json:"region,omitempty"`
	Prefix             string `json:"prefix"`
	CredentialRef      string `json:"credentialRef"`
	CredentialRevision uint64 `json:"credentialRevision"`
}
type RemoteObject struct {
	Key       string `json:"key"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	ETag      string `json:"etag,omitempty"`
	VersionID string `json:"versionId,omitempty"`
}
type remoteSecret struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken,omitempty"`
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
}

var remoteSegment = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)

func ValidateBackupDestination(d BackupDestination) error {
	u, e := url.Parse(d.Endpoint)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || strings.ContainsAny(u.Path, "%\\") || len(d.Endpoint) > 2048 || !validReference(d.CredentialRef) || d.CredentialRevision == 0 {
		return ErrRemoteRejected
	}
	if strings.ContainsAny(u.Host, "\r\n\x00") {
		return ErrRemoteRejected
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return ErrRemoteRejected
		}
	}
	if d.Kind != "s3" && d.Kind != "webdav" {
		return ErrRemoteRejected
	}
	if d.Kind == "s3" && (!regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`).MatchString(d.Bucket) || !remoteSegment.MatchString(d.Region) || u.Path != "" && u.Path != "/") {
		return ErrRemoteRejected
	}
	if d.Kind == "webdav" && (d.Bucket != "" || d.Region != "") {
		return ErrRemoteRejected
	}
	for _, path := range []string{strings.Trim(u.Path, "/"), d.Prefix} {
		if len(path) > 512 {
			return ErrRemoteRejected
		}
		if path != "" {
			for _, s := range strings.Split(path, "/") {
				if !remoteSegment.MatchString(s) || s == "." || s == ".." {
					return ErrRemoteRejected
				}
			}
		}
	}
	return nil
}
func publicRemoteIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96", "64:ff9b:1::/48"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}
func remoteHTTPClient() *http.Client {
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, ErrRemoteRejected
		}
		ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if e != nil || len(ips) == 0 || len(ips) > 32 {
			return nil, ErrRemoteRejected
		}
		for _, ip := range ips {
			if !publicRemoteIP(ip) {
				return nil, ErrRemoteRejected
			}
		}
		d := net.Dialer{Timeout: 10 * time.Second}
		var last error
		for _, ip := range ips {
			c, e := d.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if e == nil {
				return c, nil
			}
			last = e
		}
		return nil, last
	}
	tr := &http.Transport{Proxy: nil, DialContext: dial, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second, MaxResponseHeaderBytes: 32 << 10, DisableKeepAlives: true, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}, DisableCompression: true}
	return &http.Client{Transport: tr, Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type remoteStore struct {
	destination BackupDestination
	client      *http.Client
	secret      remoteSecret
	gate        func() error
}

func newRemoteStore(d BackupDestination, secret []byte, gate func() error) (*remoteStore, error) {
	if ValidateBackupDestination(d) != nil || gate == nil || len(secret) > 64<<10 {
		return nil, ErrRemoteRejected
	}
	var v remoteSecret
	if strictOffsiteJSON(secret, &v) != nil {
		return nil, ErrRemoteRejected
	}
	if strings.ContainsAny(v.AccessKeyID+v.SecretAccessKey+v.SessionToken+v.Username+v.Password, "\r\n\x00") {
		return nil, ErrRemoteRejected
	}
	if d.Kind == "s3" && (len(v.AccessKeyID) < 3 || len(v.AccessKeyID) > 128 || len(v.SecretAccessKey) < 8 || len(v.SecretAccessKey) > 256 || v.Username != "" || v.Password != "") {
		return nil, ErrRemoteRejected
	}
	if d.Kind == "webdav" && (v.Username == "" || v.Password == "" || v.AccessKeyID != "" || v.SecretAccessKey != "" || v.SessionToken != "" || strings.Contains(v.Username, ":")) {
		return nil, ErrRemoteRejected
	}
	return &remoteStore{d, remoteHTTPClient(), v, gate}, nil
}
func awsEncode(s string) string {
	const digits = "0123456789ABCDEF"
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.~", rune(c)) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(digits[c>>4])
			b.WriteByte(digits[c&15])
		}
	}
	return b.String()
}
func signRemoteRequest(r *http.Request, region string, secret remoteSecret, payload string, now time.Time) {
	stamp := now.UTC().Format("20060102T150405Z")
	day := stamp[:8]
	r.Header.Set("X-Amz-Date", stamp)
	r.Header.Set("X-Amz-Content-Sha256", payload)
	if secret.SessionToken != "" {
		r.Header.Set("X-Amz-Security-Token", secret.SessionToken)
	}
	headers := map[string]string{"host": r.URL.Host}
	for k, values := range r.Header {
		lower := strings.ToLower(k)
		if strings.HasPrefix(lower, "x-amz-") || lower == "if-match" || lower == "if-none-match" {
			headers[lower] = strings.Join(values, ",")
		}
	}
	names := []string{}
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonical strings.Builder
	for _, name := range names {
		canonical.WriteString(name + ":" + strings.Join(strings.Fields(headers[name]), " ") + "\n")
	}
	signed := strings.Join(names, ";")
	request := r.Method + "\n" + r.URL.EscapedPath() + "\n" + r.URL.RawQuery + "\n" + canonical.String() + "\n" + signed + "\n" + payload
	sum := sha256.Sum256([]byte(request))
	scope := day + "/" + region + "/s3/aws4_request"
	text := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hex.EncodeToString(sum[:])
	mac := func(key []byte, s string) []byte {
		h := hmac.New(sha256.New, key)
		h.Write([]byte(s))
		return h.Sum(nil)
	}
	key := []byte("AWS4" + secret.SecretAccessKey)
	for _, s := range []string{day, region, "s3", "aws4_request"} {
		next := mac(key, s)
		clear(key)
		key = next
	}
	sig := mac(key, text)
	clear(key)
	r.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+secret.AccessKeyID+"/"+scope+", SignedHeaders="+signed+", Signature="+hex.EncodeToString(sig))
	clear(sig)
}
func (s *remoteStore) request(ctx context.Context, method string, o RemoteObject, body io.Reader) (*http.Response, error) {
	if s.gate() != nil || len(o.Key) > 768 || o.Bytes <= 0 || o.Bytes > MaxBackupBundleBytes || !validDigest(o.SHA256) {
		return nil, ErrRemoteRejected
	}
	for _, p := range strings.Split(o.Key, "/") {
		if !remoteSegment.MatchString(p) || p == "." || p == ".." {
			return nil, ErrRemoteRejected
		}
	}
	u, e := url.Parse(s.destination.Endpoint)
	if e != nil {
		return nil, ErrRemoteRejected
	}
	base := strings.TrimRight(u.Path, "/")
	if s.destination.Kind == "s3" {
		base += "/" + s.destination.Bucket
	}
	u.Path = base + "/" + o.Key
	if o.VersionID != "" {
		if s.destination.Kind != "s3" || len(o.VersionID) > 2048 || strings.ContainsAny(o.VersionID, "\r\n\x00") {
			return nil, ErrRemoteRejected
		}
		u.RawQuery = "versionId=" + awsEncode(o.VersionID)
	}
	req, e := http.NewRequestWithContext(ctx, method, u.String(), body)
	if e != nil {
		return nil, ErrRemoteRejected
	}
	req.GetBody = nil
	empty := sha256.Sum256(nil)
	payload := hex.EncodeToString(empty[:])
	if method == http.MethodPut {
		req.ContentLength = o.Bytes
		payload = o.SHA256
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("If-None-Match", "*")
	}
	if method == http.MethodDelete {
		if !strongRemoteETag(o.ETag) {
			return nil, ErrRemoteRejected
		}
		req.Header.Set("If-Match", o.ETag)
	}
	if s.destination.Kind == "s3" {
		signRemoteRequest(req, s.destination.Region, s.secret, payload, time.Now())
	} else {
		req.SetBasicAuth(s.secret.Username, s.secret.Password)
	}
	defer req.Header.Del("Authorization")
	defer req.Header.Del("X-Amz-Security-Token")
	response, e := s.client.Do(req)
	if e != nil {
		return nil, ErrRemoteUnknown
	}
	if s.gate() != nil {
		response.Body.Close()
		return nil, ErrRemoteUnknown
	}
	return response, nil
}
func strongRemoteETag(s string) bool {
	if len(s) < 2 || len(s) > 256 || s[0] != '"' || s[len(s)-1] != '"' {
		return false
	}
	for _, c := range []byte(s[1 : len(s)-1]) {
		if c < 0x21 || c == 0x22 || c == 0x7f {
			return false
		}
	}
	return true
}
func (s *remoteStore) put(ctx context.Context, o RemoteObject, input io.Reader) (RemoteObject, error) {
	r, e := s.request(ctx, http.MethodPut, o, &offsiteReader{r: io.LimitReader(input, o.Bytes+1), gate: s.gate})
	if e != nil {
		return o, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 && r.StatusCode != 201 && r.StatusCode != 204 {
		return o, ErrRemoteUnknown
	}
	_, e = io.Copy(io.Discard, io.LimitReader(r.Body, 64<<10))
	if e != nil {
		return o, ErrRemoteUnknown
	}
	o.ETag = r.Header.Get("ETag")
	o.VersionID = r.Header.Get("X-Amz-Version-Id")
	if len(o.VersionID) > 2048 {
		return o, ErrRemoteUnknown
	}
	return s.verify(ctx, o)
}
func (s *remoteStore) verify(ctx context.Context, o RemoteObject) (RemoteObject, error) {
	r, e := s.request(ctx, http.MethodGet, o, nil)
	if e != nil {
		return o, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 || r.ContentLength >= 0 && r.ContentLength != o.Bytes || r.Header.Get("Content-Encoding") != "" {
		return o, ErrRemoteUnknown
	}
	h := sha256.New()
	n, e := io.Copy(h, &offsiteReader{r: io.LimitReader(r.Body, o.Bytes+1), gate: s.gate})
	if e != nil || n != o.Bytes || hex.EncodeToString(h.Sum(nil)) != o.SHA256 || s.gate() != nil {
		return o, ErrRemoteUnknown
	}
	if etag := r.Header.Get("ETag"); strongRemoteETag(etag) {
		o.ETag = etag
	} else {
		o.ETag = ""
	}
	if version := r.Header.Get("X-Amz-Version-Id"); version != "" {
		if o.VersionID != "" && version != o.VersionID {
			return o, ErrRemoteUnknown
		}
		o.VersionID = version
	}
	return o, nil
}
func (s *remoteStore) remove(ctx context.Context, o RemoteObject) error {
	if !strongRemoteETag(o.ETag) {
		return ErrRemoteRejected
	}
	checked, e := s.verify(ctx, o)
	if e != nil || checked.ETag != o.ETag || checked.VersionID != o.VersionID {
		return ErrRemoteUnknown
	}
	r, e := s.request(ctx, http.MethodDelete, o, nil)
	if e != nil {
		return e
	}
	io.Copy(io.Discard, io.LimitReader(r.Body, 64<<10))
	r.Body.Close()
	if r.StatusCode != 200 && r.StatusCode != 202 && r.StatusCode != 204 {
		return ErrRemoteUnknown
	}
	r, e = s.request(ctx, http.MethodGet, o, nil)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode != 404 || s.gate() != nil {
		return ErrRemoteUnknown
	}
	return nil
}

type offsiteReader struct {
	r    io.Reader
	gate func() error
}

func (r *offsiteReader) Read(p []byte) (int, error) {
	if r.gate() != nil {
		return 0, ErrRemoteUnknown
	}
	if len(p) > 64<<10 {
		p = p[:64<<10]
	}
	return r.r.Read(p)
}
func strictOffsiteJSON(raw []byte, v any) error {
	// Credential JSON has a flat, exact, case-sensitive key set. Reject duplicate
	// fields before decoding so aliases cannot silently replace a secret.
	keys := map[string]bool{}
	allowed := map[string]bool{"accessKeyId": true, "secretAccessKey": true, "sessionToken": true, "username": true, "password": true}
	check := json.NewDecoder(strings.NewReader(string(raw)))
	tok, e := check.Token()
	if e != nil || tok != json.Delim('{') {
		return ErrRemoteRejected
	}
	for check.More() {
		k, e := check.Token()
		key, ok := k.(string)
		if e != nil || !ok || keys[key] || !allowed[key] {
			return ErrRemoteRejected
		}
		keys[key] = true
		var value string
		if check.Decode(&value) != nil {
			return ErrRemoteRejected
		}
	}
	if _, e = check.Token(); e != nil {
		return ErrRemoteRejected
	}
	if check.Decode(&struct{}{}) != io.EOF {
		return ErrRemoteRejected
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(&struct{}{}) != io.EOF {
		return ErrRemoteRejected
	}
	return nil
}
func destinationHash(d BackupDestination) string {
	d.CredentialRef = ""
	d.CredentialRevision = 0
	raw, _ := json.Marshal(d)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func remoteObjectKey(d BackupDestination, authority string, b BackupMetadata) string {
	key := offsiteKey(authority)[:16] + "-" + b.ID + "-" + b.SHA256 + ".age"
	if d.Prefix != "" {
		return d.Prefix + "/" + key
	}
	return key
}
func offsiteKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
func offsiteNumber(v uint64) string { return strconv.FormatUint(v, 10) }
