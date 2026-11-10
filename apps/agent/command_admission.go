package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

func guardedCommandAction(action agentAction, node string, endpoint agentLinkEndpoint, config *tls.Config) agentAction {
	run := action.Run
	action.Run = func(ctx context.Context, raw []byte) executionResult {
		unknown := executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		offer, ok := ctx.Value(agentOfferKey{}).(*agentv1.TaskOffer)
		if !ok {
			return unknown
		}
		transport := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, MaxResponseHeaderBytes: 8 << 10, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second, TLSClientConfig: config.Clone(), DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != endpoint.authority {
				return nil, errAgentConnect
			}
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, endpoint.address)
		}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errExecutionRejected }}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return unknown
		}
		text := hex.EncodeToString(nonce[:])
		live := func(ctx context.Context, phase string) error {
			payload, _ := json.Marshal(struct {
				NodeID string             `json:"nodeId"`
				Nonce  string             `json:"nonce"`
				Phase  string             `json:"phase"`
				Offer  *agentv1.TaskOffer `json:"offer"`
			}{node, text, phase, offer})
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+endpoint.authority+"/api/v1/agent/command-admission", bytes.NewReader(payload))
			if err != nil {
				return err
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := client.Do(request)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			var receipt map[string]string
			if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&receipt) != nil || len(receipt) != 6 || receipt["taskId"] != offer.TaskId || receipt["executionId"] != offer.ExecutionId || receipt["planHash"] != offer.PlanDigestSha256 || receipt["nonce"] != text || receipt["phase"] != phase || receipt["timeoutSeconds"] != strconv.Itoa(int(offer.TimeoutSeconds)) {
				return errExecutionRejected
			}
			return nil
		}
		if waitCommandAdmission(ctx, live) != nil || live(ctx, "start") != nil {
			return unknown
		}
		guarded, cancel := context.WithCancel(ctx)
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
		result := run(guarded, raw)
		if live(guarded, "current") != nil {
			return unknown
		}
		return result
	}
	return action
}
func waitCommandAdmission(ctx context.Context, live func(context.Context, string) error) error {
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := live(ctx, "current"); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errExecutionRejected
		case <-ticker.C:
		}
	}
}
