package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
	"time"
)

type updateMailboxConfig struct {
	Format       int    `json:"format"`
	NodeID       string `json:"nodeId"`
	Directory    string `json:"directory"`
	PolicySHA256 string `json:"policySha256"`
}

func registeredResidentUpdate(filename, node string) (agentAction, error) {
	var config updateMailboxConfig
	raw, e := readCommandPrivateFile(filename, 8192, false)
	if e != nil {
		return agentAction{}, e
	}
	d := json.NewDecoder(bytesReaderAgent(raw))
	d.DisallowUnknownFields()
	if d.Decode(&config) != nil || d.Decode(&struct{}{}) != io.EOF || config.Format != 1 || config.NodeID != node || !transport.FileDigest(config.PolicySHA256) || !filepath.IsAbs(config.Directory) {
		return agentAction{}, errExecutionRejected
	}
	check, e := readMailboxRoot(config.Directory)
	if e != nil {
		return agentAction{}, e
	}
	check.Close()
	validate := func(raw []byte) error {
		p, e := transport.ParseResidentUpdateParameters(raw)
		if e != nil || p.PolicySHA256 != config.PolicySHA256 {
			return errExecutionRejected
		}
		return nil
	}
	return agentAction{Validate: validate, Run: func(ctx context.Context, raw []byte) executionResult {
		unknown := executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		offer, ok := ctx.Value(agentOfferKey{}).(*agentv1.TaskOffer)
		if !ok || offer.ActionId != transport.ResidentUpdateAction || validate(raw) != nil {
			return unknown
		}
		root, e := readMailboxRoot(config.Directory)
		if e != nil {
			return unknown
		}
		defer root.Close()
		intent := transport.ResidentUpdateIntent{Format: 1, TaskID: offer.TaskId, ExecutionID: offer.ExecutionId, PlanSHA256: offer.PlanDigestSha256, NodeID: node, Parameters: string(raw), ExpiresAt: minUpdateDeadline(offer)}
		if !intent.Valid(time.Now()) {
			return unknown
		}
		b, _ := json.Marshal(intent)
		f, e := root.OpenFile(intent.ExecutionID+".request.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return unknown
		}
		n, we := f.Write(b)
		sy := f.Sync()
		ce := f.Close()
		dir, e := root.Open(".")
		if e != nil {
			return unknown
		}
		ds := dir.Sync()
		dir.Close()
		if we != nil || n != len(b) || sy != nil || ce != nil || ds != nil {
			return unknown
		}
		// The supervisor replaces THIS process. Acceptance is not installation. Only
		// its authenticated health/commit report can finish the Controller task.
		<-ctx.Done()
		return unknown
	}}, nil
}

type agentByteReader struct{ b []byte }

func (r *agentByteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}
func bytesReaderAgent(b []byte) io.Reader { return &agentByteReader{b} }
func readMailboxRoot(directory string) (*os.Root, error) {
	info, e := os.Lstat(directory)
	if e != nil || !commandFileOwned(info, true, false) || os.Geteuid() == 0 {
		return nil, errExecutionRejected
	}
	return os.OpenRoot(directory)
}

func residentUpdateBusy(filename string) bool {
	raw, e := readCommandPrivateFile(filename, 8192, false)
	if e != nil {
		return true
	}
	var c updateMailboxConfig
	if json.Unmarshal(raw, &c) != nil {
		return true
	}
	r, e := readMailboxRoot(c.Directory)
	if e != nil {
		return true
	}
	defer r.Close()
	_, e = r.Lstat("update-active.json")
	return !os.IsNotExist(e)
}

func minUpdateDeadline(o *agentv1.TaskOffer) time.Time {
	deadline := time.UnixMilli(o.ExpiresAtUnixMs)
	timeout := time.Now().Add(time.Duration(o.TimeoutSeconds)*time.Second - 5*time.Second)
	if timeout.Before(deadline) {
		return timeout
	}
	return deadline
}
