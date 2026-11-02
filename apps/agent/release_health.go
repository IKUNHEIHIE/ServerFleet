package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Set only by the release build (-X main.agentBuildVersion=...), never by HTTP,
// enrollment metadata or the optional -agent-version compatibility label.
var agentBuildVersion = "0.9.0-dev"

type agentReleaseHealth struct {
	Version         string    `json:"version"`
	PID             int       `json:"pid"`
	Nonce           string    `json:"nonce"`
	NodeID          string    `json:"nodeId"`
	ConnectionID    string    `json:"connectionId"`
	StateFormat     int       `json:"stateFormat"`
	TrustGeneration uint64    `json:"trustGeneration"`
	ObservedAt      time.Time `json:"observedAt"`
}

// An explicitly owned supervisor may request a PRIVATE readiness report. It is
// emitted only after real identity/ledger loading and successful WSS bootstrap.
// No loopback endpoint, executable command or secret reader is introduced.
func startAgentReleaseHealth(ctx context.Context, facts agentIdentityFacts, connection string) (func(), error) {
	filename := os.Getenv("SF_AGENT_HEALTH_FILE")
	if filename == "" {
		return func() {}, nil
	}
	nonce := os.Getenv("SF_SUPERVISOR_NONCE")
	if !filepath.IsAbs(filename) || len(nonce) != 32 || !executionIDPattern.MatchString(nonce) {
		return nil, errAgentConnect
	}
	root, err := openAgentHealthDirectory(filepath.Dir(filename))
	if err != nil {
		return nil, err
	}
	name := filepath.Base(filename)
	write := func() error {
		if info, e := root.Lstat(name); e == nil && !info.Mode().IsRegular() {
			return errAgentConnect
		} else if e != nil && !os.IsNotExist(e) {
			return errAgentConnect
		}
		raw, _ := json.Marshal(agentReleaseHealth{agentBuildVersion, os.Getpid(), nonce, facts.NodeID, connection, 1, facts.TrustGeneration, time.Now().UTC()})
		switch e := root.Remove(name + ".tmp"); {
		case e != nil && !os.IsNotExist(e):
			return errAgentConnect
		}
		file, e := root.OpenFile(name+".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = file.Write(raw)
		syncErr := file.Sync()
		closeErr := file.Close()
		if e != nil || syncErr != nil || closeErr != nil {
			return errAgentConnect
		}
		return root.Rename(name+".tmp", name)
	}
	if write() != nil {
		root.Close()
		return nil, errAgentConnect
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if write() != nil {
					return
				}
			}
		}
	}()
	return func() { once.Do(func() { close(stop); <-done; root.Remove(name); root.Close() }) }, nil
}
