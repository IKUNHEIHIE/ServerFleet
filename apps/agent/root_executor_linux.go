package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

type rootHelperRegistration struct {
	Version           int    `json:"version"`
	NodeID            string `json:"nodeId"`
	SudoExecutable    string `json:"sudoExecutable"`
	SudoSHA256        string `json:"sudoSha256"`
	HelperExecutable  string `json:"helperExecutable"`
	HelperSHA256      string `json:"helperSha256"`
	RootConfiguration string `json:"rootConfiguration"`
	PolicySHA256      string `json:"policySha256"`
}
type rootExecutorConfiguration struct {
	Version           int    `json:"version"`
	NodeID            string `json:"nodeId"`
	Profiles          string `json:"profiles"`
	Ledger            string `json:"ledger"`
	ControllerOrigin  string `json:"controllerOrigin"`
	RootCertificate   string `json:"rootCertificate"`
	RootPin           string `json:"rootPin"`
	ClientCertificate string `json:"clientCertificate"`
	ClientKey         string `json:"clientKey"`
}

func rootExecutable(filename, digest string) (*os.File, error) {
	if !filepath.IsAbs(filename) || filepath.Clean(filename) != filename || !transport.FileDigest(digest) {
		return nil, errExecutionRejected
	}
	if !rootCommandAncestors(filepath.Dir(filename)) {
		return nil, errExecutionRejected
	}
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() < 1 || info.Size() > 256<<20 {
		return nil, errExecutionRejected
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return nil, errExecutionRejected
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		file.Close()
		return nil, errExecutionRejected
	}
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(file, (256<<20)+1)); err != nil || hex.EncodeToString(h.Sum(nil)) != digest {
		file.Close()
		return nil, errExecutionRejected
	}
	return file, nil
}

type boundedRootResult struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	overflow bool
}

func (b *boundedRootResult) Write(raw []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(raw)
	if b.buf.Len()+n > 4096 {
		b.overflow = true
		return n, nil
	}
	return b.buf.Write(raw)
}
func registeredRootHelper(filename, node string) (agentAction, error) {
	if os.Geteuid() == 0 {
		return agentAction{}, errExecutionRejected
	}
	raw, err := readCommandPrivateFile(filename, 16<<10, false)
	if err != nil {
		return agentAction{}, err
	}
	var cfg rootHelperRegistration
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cfg) != nil || decoder.Decode(&struct{}{}) != io.EOF || cfg.Version != 1 || cfg.NodeID != node || !filepath.IsAbs(cfg.RootConfiguration) || !transport.FileDigest(cfg.PolicySHA256) {
		return agentAction{}, errExecutionRejected
	}
	cfgSHA := sha256.Sum256(raw)
	validate := func(parameters []byte) error {
		p, e := transport.ParseCommandParameters(parameters)
		if e != nil || p.PolicySHA256 != cfg.PolicySHA256 {
			return errExecutionRejected
		}
		return nil
	}
	return agentAction{Privilege: agentv1.Privilege_PRIVILEGE_ROOT, Validate: validate, Run: func(ctx context.Context, parameters []byte) executionResult {
		unknown := executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		offer, ok := ctx.Value(agentOfferKey{}).(*agentv1.TaskOffer)
		current, e := readCommandPrivateFile(filename, 16<<10, false)
		if !ok || e != nil || sha256.Sum256(current) != cfgSHA || validate(parameters) != nil || offer.Privilege != agentv1.Privilege_PRIVILEGE_ROOT {
			return unknown
		}
		sudo, e := rootExecutable(cfg.SudoExecutable, cfg.SudoSHA256)
		if e != nil {
			return unknown
		}
		defer sudo.Close()
		helper, e := rootExecutable(cfg.HelperExecutable, cfg.HelperSHA256)
		if e != nil {
			return unknown
		}
		helper.Close()
		payload, _ := json.Marshal(offer)
		run, cancel := context.WithTimeout(ctx, time.Duration(offer.TimeoutSeconds+10)*time.Second)
		defer cancel()
		cmd := exec.CommandContext(run, "/proc/self/fd/3", "-n", "--", cfg.HelperExecutable, "root-executor", "-config", cfg.RootConfiguration)
		cmd.ExtraFiles = []*os.File{sudo}
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
		cmd.Stdin = bytes.NewReader(payload)
		cmd.Stderr = io.Discard
		output := &boundedRootResult{}
		cmd.Stdout = output
		cmd.WaitDelay = time.Second
		if cmd.Run() != nil || run.Err() != nil || output.overflow {
			return unknown
		}
		var result executionResult
		decoder := json.NewDecoder(bytes.NewReader(output.buf.Bytes()))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&result) != nil || decoder.Decode(&struct{}{}) != io.EOF || !validExecutionResult(result) || len(result.Output) > 0 {
			return unknown
		}
		return result
	}}, nil
}
func runRootExecutor(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if os.Geteuid() != 0 {
		return 2
	}
	flags := flag.NewFlagSet("root-executor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	filename := flags.String("config", "", "root-owned private executor configuration")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return 2
	}
	raw, err := readCommandPrivateFile(*filename, 16<<10, true)
	if err != nil {
		return 2
	}
	var cfg rootExecutorConfiguration
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cfg) != nil || decoder.Decode(&struct{}{}) != io.EOF || cfg.Version != 1 || !transport.FileID(cfg.NodeID) || !filepath.IsAbs(cfg.Ledger) {
		return 2
	}
	policy, digest, err := loadCommandProfiles(cfg.Profiles, cfg.NodeID, true)
	if err != nil {
		return 2
	}
	info, err := os.Lstat(cfg.Ledger)
	if err != nil || !commandFileOwned(info, true, true) || !rootCommandAncestors(cfg.Ledger) {
		return 2
	}
	payload, err := io.ReadAll(io.LimitReader(stdin, (24<<10)+1))
	if err != nil || len(payload) > 24<<10 {
		return 2
	}
	var offer agentv1.TaskOffer
	decoder = json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&offer) != nil || decoder.Decode(&struct{}{}) != io.EOF || offer.ActionId != transport.RootCommandCapability || offer.Privilege != agentv1.Privilege_PRIVILEGE_ROOT {
		return 2
	}
	params, err := transport.ParseCommandParameters(offer.CanonicalParametersJson)
	if err != nil || params.PolicySHA256 != digest || uint32(params.TimeoutSeconds) != offer.TimeoutSeconds {
		return 2
	}
	var selected *commandProfile
	for i := range policy.Profiles {
		p := &policy.Profiles[i]
		if p.ID == params.ProfileID && len(params.Arguments) <= p.MaximumArguments && params.TimeoutSeconds <= p.MaximumSeconds {
			selected = p
		}
	}
	if selected == nil {
		return 2
	}
	client, origin, err := rootAdmissionClient(cfg)
	if err != nil {
		return 2
	}
	defer client.CloseIdleConnections()
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return 2
	}
	nonceText := hex.EncodeToString(nonce[:])
	live := func(current context.Context, phase string) error {
		data, _ := json.Marshal(struct {
			NodeID string             `json:"nodeId"`
			Nonce  string             `json:"nonce"`
			Phase  string             `json:"phase"`
			Offer  *agentv1.TaskOffer `json:"offer"`
		}{cfg.NodeID, nonceText, phase, &offer})
		request, e := http.NewRequestWithContext(current, http.MethodPost, origin+"/api/v1/agent/root-admission", bytes.NewReader(data))
		if e != nil {
			return e
		}
		request.Header.Set("Content-Type", "application/json")
		response, e := client.Do(request)
		if e != nil {
			return e
		}
		defer response.Body.Close()
		var receipt map[string]string
		if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&receipt) != nil || len(receipt) != 6 || receipt["taskId"] != offer.TaskId || receipt["executionId"] != offer.ExecutionId || receipt["planHash"] != offer.PlanDigestSha256 || receipt["nonce"] != nonceText || receipt["phase"] != phase || receipt["timeoutSeconds"] != strconv.Itoa(params.TimeoutSeconds) {
			return errExecutionRejected
		}
		return nil
	}
	if waitCommandAdmission(ctx, live) != nil || live(ctx, "start") != nil {
		return 2
	}
	action := agentAction{Privilege: agentv1.Privilege_PRIVILEGE_ROOT, Validate: func(raw []byte) error {
		p, e := transport.ParseCommandParameters(raw)
		if e != nil || p.ProfileID != params.ProfileID || p.PolicySHA256 != digest {
			return errExecutionRejected
		}
		return nil
	}, Run: func(run context.Context, _ []byte) executionResult {
		guarded, cancel := context.WithCancel(run)
		defer cancel()
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-guarded.Done():
					return
				case <-ticker.C:
					if live(guarded, "current") != nil {
						cancel()
						return
					}
				}
			}
		}()
		if live(guarded, "current") != nil {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		}
		result := runPinnedCommand(guarded, *selected, params.Arguments, params.TimeoutSeconds, true)
		if live(guarded, "current") != nil {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		}
		return result
	}}
	executor, err := newAgentExecutor(cfg.Ledger, map[string]agentAction{transport.RootCommandCapability: action})
	if err != nil {
		return 2
	}
	defer executor.Close()
	result, _, err := executor.execute(ctx, &offer)
	if err != nil {
		return 2
	}
	if json.NewEncoder(stdout).Encode(result) != nil {
		return 2
	}
	return 0
}
func rootAdmissionClient(cfg rootExecutorConfiguration) (*http.Client, string, error) {
	origin, err := url.Parse(cfg.ControllerOrigin)
	if err != nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" && origin.Path != "/" {
		return nil, "", errExecutionRejected
	}
	root, err := readCommandPrivateFile(cfg.RootCertificate, 16<<10, true)
	if err != nil {
		return nil, "", err
	}
	pool, pin, err := parseAgentConnectionRoot(root)
	if err != nil || pin != cfg.RootPin {
		return nil, "", errExecutionRejected
	}
	certRaw, err := readCommandPrivateFile(cfg.ClientCertificate, 32<<10, true)
	if err != nil {
		return nil, "", err
	}
	keyRaw, err := readCommandPrivateFile(cfg.ClientKey, 16<<10, true)
	if err != nil {
		return nil, "", err
	}
	defer clear(keyRaw)
	certificate, err := tls.X509KeyPair(certRaw, keyRaw)
	if err != nil {
		return nil, "", err
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil || leaf.Subject.CommonName != cfg.NodeID {
		return nil, "", errExecutionRejected
	}
	transport := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, MaxResponseHeaderBytes: 8 << 10, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, Certificates: []tls.Certificate{certificate}}}
	return &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errExecutionRejected }}, origin.Scheme + "://" + origin.Host, nil
}

func rootCommandAncestors(directory string) bool {
	// Clean before inspecting would hide components traversed by the OS (for
	// example a symlink followed by ".."). Accept only the exact canonical path.
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return false
	}
	for {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return false
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return false
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return true
		}
		directory = parent
	}
}

func inspectCommandPolicy(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("command-policy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "private canonical profile JSON")
	node := flags.String("node", "", "exact enrolled node ID")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return 2
	}
	profiles, digest, err := loadCommandProfiles(*file, *node, os.Geteuid() == 0)
	if err != nil {
		return 2
	}
	ids := make([]string, 0, len(profiles.Profiles))
	for _, profile := range profiles.Profiles {
		ids = append(ids, profile.ID)
	}
	if json.NewEncoder(stdout).Encode(map[string]any{"nodeId": *node, "policySha256": digest, "profileIds": ids, "rootOwned": os.Geteuid() == 0, "executed": false}) != nil {
		return 2
	}
	return 0
}
