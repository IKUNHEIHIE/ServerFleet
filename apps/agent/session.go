package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"runtime"
	"slices"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

var errAgentSession = errors.New("agent: authenticated session unavailable")
var errAgentWorkersUnknown = errors.New("agent: disconnected workers have not stopped; no automatic reconnect")
var agentProcessStartedAt = time.Now()

type agentSessionConfig struct {
	NodeID, BootID, Version, ServerName, RootSPKISHA256 string
	ProtocolMajor, ProtocolMinor                        uint32
	MaxConcurrent                                       int
	Semantic                                            transport.ValidationLimits
	Frames                                              transport.FrameDecodeLimits
	// Current reads the actual local trust/revocation/profile authorities. A
	// cached TLS state alone cannot admit a long-lived connection indefinitely.
	Current func(context.Context, tls.ConnectionState) error
	// Admit rechecks the current LOCAL capability/path/privilege policy. It is
	// additional to the Controller's sole authorization/task/approval admission.
	Admit func(context.Context, *agentv1.TaskOffer) error
	// Probe must be an authenticated WSS ping/pong with bounded cancellation.
	Probe func(context.Context) error
	// InitialSample is an optional one-shot telemetry sample. When set, the
	// session writes one Heartbeat and the Metrics batch immediately after the
	// handshake; nil keeps the ticker-only behavior of the long-lived profile.
	InitialSample func(context.Context) (*agentv1.MetricsBatch, error)
	// Established and ExecutionFinished are program-owned notifications for the
	// one-shot CLI. They must not block; they never replace transport delivery.
	Established       func()
	ExecutionFinished func(taskID, executionID string)
	Files             *agentFileSessions
}
type activeAgentExecution struct {
	residentUpdate    bool
	taskID, digest    string
	cancel            context.CancelFunc
	reserved          bool
	started, finished bool
}

// runAgentSession owns ONE verified Agent-initiated connection. The reviewed
// dialer supplies socket/TLS/header-derived connectionID; no URL/TLS bypass or
// retry loop is provided here, and main does not enable an unaccepted profile.
func callSessionNotification(fn func()) (err error) {
	defer func() {
		if recover() != nil {
			err = errAgentSession
		}
	}()
	fn()
	return nil
}

func runAgentSession(ctx context.Context, socket transport.FrameSocket, state tls.ConnectionState, connectionID string, cfg agentSessionConfig, executor *agentExecutor) (result error) {
	if socket != nil {
		defer socket.Close()
	}
	if ctx == nil || ctx.Err() != nil || socket == nil || executor == nil || executor.ledger == nil || !executionIDPattern.MatchString(cfg.NodeID) || !executionIDPattern.MatchString(cfg.BootID) || !executionIDPattern.MatchString(connectionID) || cfg.Version == "" || cfg.ServerName == "" || cfg.ProtocolMajor == 0 || cfg.MaxConcurrent < 1 || cfg.MaxConcurrent > 5 || cfg.Current == nil || cfg.Admit == nil || cfg.Probe == nil || cfg.Frames.MaxFrameBytes <= 0 || cfg.Frames.MaxFrameBytes > 1<<20 || cfg.Semantic.MaxMessageBytes <= 0 || cfg.Semantic.MaxMessageBytes > cfg.Frames.MaxFrameBytes || cfg.Semantic.MaxOutputBytes > 64<<10 || cfg.Semantic.MaxParametersBytes > 64<<10 || cfg.Semantic.MaxOfferLifetime > 15*time.Minute {
		return errAgentSession
	}
	// Caller-owned policies are copied before asynchronous read/heartbeat/work.
	actions := map[string]string{}
	for id, version := range cfg.Semantic.AllowedActions {
		if _, exists := executor.actions[id]; !exists || version == "" {
			return errAgentSession
		}
		actions[id] = version
	}
	cfg.Semantic.AllowedActions = actions
	rootPin, err := hex.DecodeString(cfg.RootSPKISHA256)
	if err != nil || len(rootPin) != 32 || hex.EncodeToString(rootPin) != cfg.RootSPKISHA256 {
		return errAgentSession
	}
	bounded, cancel := context.WithCancel(ctx)
	defer cancel()
	current := func() (observationErr error) {
		defer func() {
			if recover() != nil {
				observationErr = errAgentSession
			}
		}()
		if bounded.Err() != nil || !state.HandshakeComplete || state.Version < tls.VersionTLS13 || state.ServerName != cfg.ServerName || len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 || len(state.PeerCertificates) == 0 {
			return errAgentSession
		}
		chain := state.VerifiedChains[0]
		pin := sha256.Sum256(chain[len(chain)-1].RawSubjectPublicKeyInfo)
		now := time.Now()
		if hex.EncodeToString(pin[:]) != cfg.RootSPKISHA256 || now.Before(state.PeerCertificates[0].NotBefore) || !now.Before(state.PeerCertificates[0].NotAfter) {
			return errAgentSession
		}
		observe, stop := context.WithTimeout(bounded, 5*time.Second)
		defer stop()
		if cfg.Current(observe, state) != nil || observe.Err() != nil {
			return errAgentSession
		}
		return nil
	}
	admit := func(ctx context.Context, offer *agentv1.TaskOffer) (admissionErr error) {
		defer func() {
			if recover() != nil {
				admissionErr = errAgentSession
			}
		}()
		bounded, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		if cfg.Admit(bounded, proto.Clone(offer).(*agentv1.TaskOffer)) != nil || bounded.Err() != nil {
			return errAgentSession
		}
		return nil
	}
	if current() != nil {
		return errAgentSession
	}
	validation := func(direction transport.Direction, phase transport.HandshakePhase) transport.ValidationContext {
		return transport.ValidationContext{Direction: direction, Phase: phase, ProtocolMajor: cfg.ProtocolMajor, ProtocolMinor: cfg.ProtocolMinor, Now: time.Now()}
	}
	guard, err := transport.NewConnectionGuard(transport.ConnectionBinding{NodeID: cfg.NodeID, ConnectionID: connectionID, Direction: transport.ControllerToAgent})
	if err != nil {
		return errAgentSession
	}
	defer guard.Close()
	var sendMu sync.Mutex
	sequence := uint64(0)
	frameLimit := cfg.Frames.MaxFrameBytes
	write := func(envelope *agentv1.Envelope, phase transport.HandshakePhase) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		if current() != nil {
			return errAgentSession
		}
		messageID, err := transport.NewConnectionID()
		if err != nil {
			return errAgentSession
		}
		sequence++
		if sequence == 0 {
			return errAgentSession
		}
		envelope.NodeId, envelope.ConnectionId, envelope.MessageId = cfg.NodeID, connectionID, messageID
		envelope.ProtocolMajor, envelope.ProtocolMinor = cfg.ProtocolMajor, cfg.ProtocolMinor
		envelope.Sequence, envelope.SentAtUnixMs = sequence, time.Now().UnixMilli()
		if transport.ValidateEnvelope(envelope, validation(transport.AgentToController, phase), cfg.Semantic) != nil {
			return errAgentSession
		}
		raw, err := proto.Marshal(envelope)
		if err != nil || len(raw) > frameLimit {
			return errAgentSession
		}
		writeCtx, stop := context.WithTimeout(bounded, 5*time.Second)
		defer stop()
		if socket.Write(writeCtx, raw) != nil {
			return transport.ErrDeliveryUnknown
		}
		return nil
	}
	capabilities := []string{}
	for id := range actions {
		capabilities = append(capabilities, id)
	}
	slices.Sort(capabilities)
	hello := &agentv1.AgentHello{AgentVersion: cfg.Version, Platform: runtime.GOOS, Architecture: runtime.GOARCH, CapabilityIds: capabilities, MinProtocolMajor: cfg.ProtocolMajor, MaxProtocolMajor: cfg.ProtocolMajor, BootId: cfg.BootID}
	if write(&agentv1.Envelope{Message: &agentv1.Envelope_AgentHello{AgentHello: hello}}, transport.AwaitAgentHello) != nil {
		return errAgentSession
	}
	handshake, stop := context.WithTimeout(bounded, 5*time.Second)
	binary, raw, err := socket.Read(handshake)
	stop()
	if err != nil || !binary || current() != nil {
		return errAgentSession
	}
	envelope, err := guard.Receive(raw, validation(transport.ControllerToAgent, transport.AwaitControllerHello), cfg.Semantic, cfg.Frames)
	if err != nil || envelope.GetControllerHello() == nil || !envelope.GetControllerHello().Accepted {
		return errAgentSession
	}
	accepted := envelope.GetControllerHello()
	if accepted.HeartbeatIntervalSeconds > 60 || accepted.MaxFrameBytes > uint32(cfg.Frames.MaxFrameBytes) {
		return errAgentSession
	}
	frameLimit = int(accepted.MaxFrameBytes)
	granted := map[string]bool{}
	for _, id := range accepted.GrantedCapabilityIds {
		if actions[id] == "" {
			return errAgentSession
		}
		granted[id] = true
	}
	var runningMu sync.Mutex
	running := map[string]*activeAgentExecution{}
	var workers sync.WaitGroup
	failure := make(chan error, 1)
	fail := func(err error) {
		select {
		case failure <- err:
		default:
		}
		cancel()
		_ = socket.Close()
	}
	defer func() {
		cancel()
		_ = socket.Close()
		runningMu.Lock()
		for _, work := range running {
			work.cancel()
		}
		runningMu.Unlock()
		done := make(chan struct{})
		go func() { workers.Wait(); close(done) }()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			result = errAgentWorkersUnknown
		}
	}()
	if cfg.InitialSample != nil {
		sampleCtx, stop := context.WithTimeout(bounded, 5*time.Second)
		batch, sampleErr := cfg.InitialSample(sampleCtx)
		sampleExpired := sampleCtx.Err()
		stop()
		if sampleErr != nil || sampleExpired != nil || batch == nil {
			return errAgentSession
		}
		beat := &agentv1.Heartbeat{UptimeSeconds: uint64(time.Since(agentProcessStartedAt) / time.Second), RunningExecutions: 0, ConnectivityStatus: "connected"}
		if write(&agentv1.Envelope{Message: &agentv1.Envelope_Heartbeat{Heartbeat: beat}}, transport.Established) != nil {
			return errAgentSession
		}
		if write(&agentv1.Envelope{Message: &agentv1.Envelope_Metrics{Metrics: batch}}, transport.Established) != nil {
			return errAgentSession
		}
		if cfg.Established != nil {
			if err := callSessionNotification(cfg.Established); err != nil {
				return errAgentSession
			}
		}
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer func() {
			if recover() != nil {
				fail(errAgentSession)
			}
		}()
		ticker := time.NewTicker(time.Duration(accepted.HeartbeatIntervalSeconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-bounded.Done():
				return
			case <-ticker.C:
				probe, stop := context.WithTimeout(bounded, 5*time.Second)
				err := cfg.Probe(probe)
				probeErr := probe.Err()
				stop()
				if err != nil || probeErr != nil || current() != nil {
					fail(errAgentSession)
					return
				}
				runningMu.Lock()
				count := len(running)
				runningMu.Unlock()
				beat := &agentv1.Heartbeat{UptimeSeconds: uint64(time.Since(agentProcessStartedAt) / time.Second), RunningExecutions: uint32(count), ConnectivityStatus: "connected"}
				if write(&agentv1.Envelope{Message: &agentv1.Envelope_Heartbeat{Heartbeat: beat}}, transport.Established) != nil {
					fail(errAgentSession)
					return
				}
				if cfg.InitialSample != nil {
					sampleCtx, stop := context.WithTimeout(bounded, 5*time.Second)
					batch, sampleErr := cfg.InitialSample(sampleCtx)
					expired := sampleCtx.Err()
					stop()
					if sampleErr != nil || expired != nil || batch == nil || write(&agentv1.Envelope{Message: &agentv1.Envelope_Metrics{Metrics: batch}}, transport.Established) != nil {
						fail(errAgentSession)
						return
					}
				}
			}
		}
	}()
	ack := func(offer *agentv1.TaskOffer, allowed, seen bool, reason string) error {
		return write(&agentv1.Envelope{Message: &agentv1.Envelope_TaskAck{TaskAck: &agentv1.TaskAck{TaskId: offer.TaskId, ExecutionId: offer.ExecutionId, Accepted: allowed, PreviouslySeen: seen, ReasonCode: reason}}}, transport.Established)
	}
	control := func(id, code string) error {
		return write(&agentv1.Envelope{Message: &agentv1.Envelope_ControlError{ControlError: &agentv1.ControlError{Code: code, RelatedMessageId: id}}}, transport.Established)
	}
	for {
		binary, raw, err = socket.Read(bounded)
		if err != nil || !binary || current() != nil {
			select {
			case cause := <-failure:
				return cause
			default:
				return errAgentSession
			}
		}
		envelope, err = guard.Receive(raw, validation(transport.ControllerToAgent, transport.Established), cfg.Semantic, cfg.Frames)
		if err != nil || len(raw) > int(accepted.MaxFrameBytes) {
			return errAgentSession
		}
		switch {
		case envelope.GetFileOpenRequest() != nil || envelope.GetFileChunkRequest() != nil:
			if cfg.Files == nil || accepted.SelectedProtocolMinor < 1 || !granted[transport.FileCapability] {
				return errAgentSession
			}
			reply, after, err := cfg.Files.Handle(bounded, envelope)
			if err != nil || write(reply, transport.Established) != nil {
				return errAgentSession
			}
			if after != nil {
				after()
			}
		case envelope.GetTaskOffer() != nil:
			offer := envelope.GetTaskOffer()
			if !granted[offer.ActionId] || offer.Privilege != registeredPrivilege(executor.actions[offer.ActionId]) || offer.PluginId != "" || admit(bounded, offer) != nil {
				if ack(offer, false, false, "CAPABILITY_REJECTED") != nil {
					return errAgentSession
				}
				continue
			}
			runningMu.Lock()
			prior := running[offer.ExecutionId]
			if prior != nil && !prior.finished {
				valid, reserved := prior.taskID == offer.TaskId && prior.digest == offerDigest(offer), prior.reserved
				runningMu.Unlock()
				if !valid {
					err = ack(offer, false, true, "EXECUTION_BINDING_CONFLICT")
				} else if reserved {
					err = ack(offer, true, true, "")
				} else {
					err = control(envelope.MessageId, "EXECUTION_RESERVATION_IN_PROGRESS")
				}
				if err != nil {
					return errAgentSession
				}
				continue
			}
			if prior != nil {
				delete(running, offer.ExecutionId)
			}
			for id, completed := range running {
				if completed.finished {
					delete(running, id)
				}
			}
			exclusive := offer.ActionId == transport.ResidentUpdateAction && len(running) > 0
			for _, active := range running {
				if active.residentUpdate && !active.finished {
					exclusive = true
				}
			}
			if exclusive || len(running) >= cfg.MaxConcurrent {
				runningMu.Unlock()
				if ack(offer, false, false, "AGENT_BUSY") != nil {
					return errAgentSession
				}
				continue
			}
			workCtx, workCancel := context.WithCancel(bounded)
			work := &activeAgentExecution{taskID: offer.TaskId, digest: offerDigest(offer), cancel: workCancel, residentUpdate: offer.ActionId == transport.ResidentUpdateAction}
			running[offer.ExecutionId] = work
			runningMu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() {
					if recover() != nil {
						fail(errAgentSession)
					}
				}()
				defer workCancel()
				defer func() {
					runningMu.Lock()
					if running[offer.ExecutionId] == work {
						delete(running, offer.ExecutionId)
					}
					runningMu.Unlock()
				}()
				outcome, seen, executionErr := executor.executeObserved(workCtx, offer, func(stage string, seen bool) error {
					if current() != nil || admit(workCtx, offer) != nil {
						return errAgentSession
					}
					if stage == "accepted" {
						if transport.IsFileAction(offer.ActionId) {
							if cfg.Files == nil || accepted.SelectedProtocolMinor < 1 || cfg.Files.Accept(workCtx, offer, seen) != nil {
								return errAgentSession
							}
						}
						runningMu.Lock()
						work.reserved = true
						runningMu.Unlock()
						return ack(offer, true, seen, "")
					}
					runningMu.Lock()
					work.started = true
					runningMu.Unlock()
					return write(&agentv1.Envelope{Message: &agentv1.Envelope_TaskStarted{TaskStarted: &agentv1.TaskStarted{TaskId: offer.TaskId, ExecutionId: offer.ExecutionId, StartedAtUnixMs: time.Now().UnixMilli()}}}, transport.Established)
				})
				runningMu.Lock()
				reserved := work.reserved
				work.finished = true
				runningMu.Unlock()
				if executionErr != nil && !reserved {
					if ack(offer, false, seen, "EXECUTION_REJECTED") != nil {
						fail(errAgentSession)
					}
					return
				}
				if executionErr != nil {
					outcome = executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
				}
				outputTruncated := outcome.Truncated
				if !seen && executionErr == nil && (len(outcome.Output) > 0 || outcome.Truncated) {
					plan, stored, err := executor.prepareOutput(workCtx, offer, min(cfg.Semantic.MaxOutputBytes, frameLimit/2))
					if err != nil {
						outputTruncated = true
					} else {
						outputTruncated = plan.Truncated
						for i := 0; i < plan.Count; i++ {
							if admit(workCtx, offer) != nil {
								fail(errAgentSession)
								return
							}
							start, end := i*plan.ChunkBytes, min((i+1)*plan.ChunkBytes, plan.Bytes)
							var chunk []byte
							if start < end {
								chunk = bytes.Clone(stored.Output[start:end])
							}
							output := &agentv1.TaskOutput{TaskId: offer.TaskId, ExecutionId: offer.ExecutionId, StreamSequence: uint64(i + 1), Stream: agentv1.TaskOutput_STREAM_STDOUT, Chunk: chunk, Truncated: plan.Truncated && i == plan.Count-1}
							if write(&agentv1.Envelope{Message: &agentv1.Envelope_TaskOutput{TaskOutput: output}}, transport.Established) != nil {
								fail(transport.ErrDeliveryUnknown)
								return
							}
						}
					}
				} else if seen && len(outcome.Output) > 0 {
					// Old terminal records without a stream plan cannot pretend to
					// have produced complete logs; a replay never regenerates them.
					claim := executionClaim{offer.TaskId, offer.ExecutionId, offerDigest(offer)}
					plan, _, exists, err := executor.loadOutputPlan(workCtx, offer.ExecutionId, claim)
					outputTruncated = err != nil || !exists || plan.Truncated
				}
				finished := &agentv1.TaskFinished{TaskId: offer.TaskId, ExecutionId: offer.ExecutionId, Result: outcome.Result, ExitCode: outcome.ExitCode, FinishedAtUnixMs: time.Now().UnixMilli(), OutputTruncated: outputTruncated}
				if write(&agentv1.Envelope{Message: &agentv1.Envelope_TaskFinished{TaskFinished: finished}}, transport.Established) != nil {
					fail(transport.ErrDeliveryUnknown)
					return
				}
				if cfg.ExecutionFinished != nil {
					if err := callSessionNotification(func() { cfg.ExecutionFinished(offer.TaskId, offer.ExecutionId) }); err != nil {
						fail(errAgentSession)
					}
				}
			}()
		case envelope.GetTaskCancel() != nil:
			request := envelope.GetTaskCancel()
			runningMu.Lock()
			work := running[request.ExecutionId]
			valid := work != nil && work.taskID == request.TaskId
			if valid {
				work.cancel()
			}
			runningMu.Unlock()
			if !valid && control(envelope.MessageId, "EXECUTION_NOT_RUNNING") != nil {
				return errAgentSession
			}
		case envelope.GetStateSyncRequest() != nil:
			reply, err := executor.snapshots(bounded, envelope.GetStateSyncRequest())
			if err == nil {
				runningMu.Lock()
				for _, snapshot := range reply.Executions {
					work := running[snapshot.ExecutionId]
					if work != nil && work.taskID == snapshot.TaskId && !work.finished && snapshot.Result == agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN {
						snapshot.Started = work.started
						snapshot.Result = agentv1.ExecutionResult_EXECUTION_RESULT_UNSPECIFIED
					}
				}
				runningMu.Unlock()
			}
			if err != nil || write(&agentv1.Envelope{Message: &agentv1.Envelope_StateSyncReply{StateSyncReply: reply}}, transport.Established) != nil {
				return errAgentSession
			}
		default:
			return errAgentSession
		}
	}
}
