package transport_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

func assertCode(t *testing.T, err error, code transport.ValidationCode) {
	t.Helper()
	var failure *transport.ValidationError
	if !errors.As(err, &failure) || failure.Code != code || failure.Field == "" {
		t.Fatalf("got %v, want typed %s with field", err, code)
	}
}

func TestValidDecodedMessages(t *testing.T) {
	d := (&agentv1.Envelope{}).ProtoReflect().Descriptor().Oneofs().ByName("message")
	if d.Fields().Len() != len(examples()) {
		t.Fatal("oneof inventory changed; review valid samples")
	}
	seen := make(map[string]bool)
	for _, sample := range examples() {
		t.Run(sample.name, func(t *testing.T) {
			wire, err := proto.Marshal(sample.e)
			if err != nil {
				t.Fatal(err)
			}
			decoded := new(agentv1.Envelope)
			if err := proto.Unmarshal(wire, decoded); err != nil {
				t.Fatal(err)
			}
			before := proto.Clone(decoded)
			if err := transport.ValidateEnvelope(decoded, sample.ctx, limits()); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(decoded, before) {
				t.Fatal("validator mutated message")
			}
			field := decoded.ProtoReflect().WhichOneof(d)
			if field == nil || seen[string(field.Name())] {
				t.Fatal("missing/duplicate payload coverage")
			}
			seen[string(field.Name())] = true
		})
	}
}

// The decoder-only rejection probe failed before T01-A. The same decoded empty
// input now calls the actual validator, not a test-local allow/deny model.
func TestDecodedEmptyMessageMustBeRejected(t *testing.T) {
	e := new(agentv1.Envelope)
	if err := proto.Unmarshal(nil, e); err != nil {
		t.Fatal(err)
	}
	assertCode(t, transport.ValidateEnvelope(e, contextFor(transport.AgentToController, transport.Established), limits()), transport.MissingPayload)
}

func TestMissingPayloadAndTypedNil(t *testing.T) {
	frames := []*agentv1.Envelope{nil, envelope()}
	for _, sample := range examples() {
		e := proto.Clone(sample.e).(*agentv1.Envelope)
		field := e.ProtoReflect().WhichOneof(e.ProtoReflect().Descriptor().Oneofs().ByName("message"))
		e.ProtoReflect().Clear(field)
		frames = append(frames, e)
	}
	e := envelope()
	e.Message = (*agentv1.Envelope_TaskOffer)(nil)
	frames = append(frames, e)
	e = envelope()
	e.Message = &agentv1.Envelope_TaskOffer{}
	frames = append(frames, e)
	for _, e := range frames {
		assertCode(t, transport.ValidateEnvelope(e, contextFor(transport.AgentToController, transport.Established), limits()), transport.MissingPayload)
	}
}

func TestEveryMessageRequiresEnvelopeIDsAndVersion(t *testing.T) {
	for _, sample := range examples() {
		t.Run(sample.name, func(t *testing.T) {
			for _, mutate := range []func(*agentv1.Envelope){func(e *agentv1.Envelope) { e.MessageId = "" }, func(e *agentv1.Envelope) { e.ConnectionId = " " }, func(e *agentv1.Envelope) { e.NodeId = "\t" }} {
				e := proto.Clone(sample.e).(*agentv1.Envelope)
				mutate(e)
				assertCode(t, transport.ValidateEnvelope(e, sample.ctx, limits()), transport.MissingID)
			}
			for _, major := range []uint32{0, 999} {
				e := proto.Clone(sample.e).(*agentv1.Envelope)
				e.ProtocolMajor = major
				assertCode(t, transport.ValidateEnvelope(e, sample.ctx, limits()), transport.InvalidProtocol)
			}
			e := proto.Clone(sample.e).(*agentv1.Envelope)
			e.ProtocolMinor++
			assertCode(t, transport.ValidateEnvelope(e, sample.ctx, limits()), transport.InvalidProtocol)
		})
	}
}

func TestDirectionAndHandshake(t *testing.T) {
	for _, sample := range examples() {
		t.Run(sample.name, func(t *testing.T) {
			ctx := sample.ctx
			if ctx.Direction == transport.AgentToController {
				ctx.Direction = transport.ControllerToAgent
			} else {
				ctx.Direction = transport.AgentToController
			}
			if sample.name == "control_error" {
				if err := transport.ValidateEnvelope(sample.e, ctx, limits()); err != nil {
					t.Fatal(err)
				}
			} else {
				assertCode(t, transport.ValidateEnvelope(sample.e, ctx, limits()), transport.InvalidDirection)
			}
			ctx = sample.ctx
			if ctx.Phase == transport.Established {
				ctx.Phase = transport.AwaitAgentHello
			} else {
				ctx.Phase = transport.Established
			}
			assertCode(t, transport.ValidateEnvelope(sample.e, ctx, limits()), transport.InvalidHandshake)
		})
	}
	for _, index := range []int{4, 8} {
		sample := examples()[index]
		sample.ctx.Phase = transport.AwaitControllerHello
		assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, limits()), transport.InvalidHandshake)
	}
}

func TestInvalidContextAndExplicitLimits(t *testing.T) {
	sample := examples()[4]
	for _, mutate := range []func(*transport.ValidationContext){
		func(c *transport.ValidationContext) { c.Direction = 0 }, func(c *transport.ValidationContext) { c.Direction = 255 },
		func(c *transport.ValidationContext) { c.Phase = 0 }, func(c *transport.ValidationContext) { c.Phase = 255 },
		func(c *transport.ValidationContext) { c.ProtocolMajor = 0 }, func(c *transport.ValidationContext) { c.Now = time.Time{} },
		func(c *transport.ValidationContext) { c.Now = time.UnixMilli(0) },
	} {
		c := sample.ctx
		mutate(&c)
		assertCode(t, transport.ValidateEnvelope(sample.e, c, limits()), transport.InvalidContext)
	}
	assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, transport.ValidationLimits{}), transport.InvalidContext)
	for _, mutate := range []func(*transport.ValidationLimits){
		func(l *transport.ValidationLimits) { l.MaxMessageBytes = -1 }, func(l *transport.ValidationLimits) { l.MaxTextBytes = 0 },
		func(l *transport.ValidationLimits) { l.MaxParametersBytes = 0 }, func(l *transport.ValidationLimits) { l.MaxOutputBytes = 0 },
		func(l *transport.ValidationLimits) { l.MaxMetrics = 0 }, func(l *transport.ValidationLimits) { l.MaxSyncExecutions = 0 },
		func(l *transport.ValidationLimits) { l.MaxCapabilities = 0 }, func(l *transport.ValidationLimits) { l.MaxTimeoutSeconds = 0 },
		func(l *transport.ValidationLimits) { l.MaxOfferLifetime = time.Nanosecond },
	} {
		l := limits()
		mutate(&l)
		assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, l), transport.InvalidContext)
	}
}

func TestTaskOfferRejections(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*agentv1.TaskOffer)
		code   transport.ValidationCode
	}{
		{"task_id", func(m *agentv1.TaskOffer) { m.TaskId = "" }, transport.MissingID},
		{"execution_id", func(m *agentv1.TaskOffer) { m.ExecutionId = "" }, transport.MissingID},
		{"digest_missing", func(m *agentv1.TaskOffer) { m.PlanDigestSha256 = "" }, transport.MissingID},
		{"digest_short", func(m *agentv1.TaskOffer) { m.PlanDigestSha256 = "abc" }, transport.InvalidField},
		{"digest_nonhex", func(m *agentv1.TaskOffer) { m.PlanDigestSha256 = strings.Repeat("g", 64) }, transport.InvalidField},
		{"action_missing", func(m *agentv1.TaskOffer) { m.ActionId = "" }, transport.MissingID},
		{"action_unknown", func(m *agentv1.TaskOffer) { m.ActionId = "other" }, transport.ActionNotAllowed},
		{"schema_missing", func(m *agentv1.TaskOffer) { m.ActionSchemaVersion = "" }, transport.MissingID},
		{"schema_unknown", func(m *agentv1.TaskOffer) { m.ActionSchemaVersion = "v2" }, transport.ActionNotAllowed},
		{"privilege_zero", func(m *agentv1.TaskOffer) { m.Privilege = 0 }, transport.UnknownEnum},
		{"privilege_unknown", func(m *agentv1.TaskOffer) { m.Privilege = 999 }, transport.UnknownEnum},
		{"json_missing", func(m *agentv1.TaskOffer) { m.CanonicalParametersJson = nil }, transport.InvalidField},
		{"json_broken", func(m *agentv1.TaskOffer) { m.CanonicalParametersJson = []byte(`{"key":`) }, transport.InvalidField},
		{"json_trailing", func(m *agentv1.TaskOffer) { m.CanonicalParametersJson = []byte(`{} {}`) }, transport.InvalidField},
		{"json_null", func(m *agentv1.TaskOffer) { m.CanonicalParametersJson = []byte(`null`) }, transport.InvalidField},
		{"json_array", func(m *agentv1.TaskOffer) { m.CanonicalParametersJson = []byte(`[]`) }, transport.InvalidField},
		{"json_scalar", func(m *agentv1.TaskOffer) { m.CanonicalParametersJson = []byte(`1`) }, transport.InvalidField},
		{"json_invalid_utf8", func(m *agentv1.TaskOffer) {
			m.CanonicalParametersJson = []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}
		}, transport.InvalidField},
		{"json_limit", func(m *agentv1.TaskOffer) {
			m.CanonicalParametersJson = []byte(`{"x":"` + strings.Repeat("a", 512) + `"}`)
		}, transport.LimitExceeded},
		{"expiry_missing", func(m *agentv1.TaskOffer) { m.ExpiresAtUnixMs = 0 }, transport.ExpiredOffer},
		{"expiry_past", func(m *agentv1.TaskOffer) { m.ExpiresAtUnixMs = fixedNow.UnixMilli() - 1 }, transport.ExpiredOffer},
		{"expiry_equal", func(m *agentv1.TaskOffer) { m.ExpiresAtUnixMs = fixedNow.UnixMilli() }, transport.ExpiredOffer},
		{"expiry_limit", func(m *agentv1.TaskOffer) { m.ExpiresAtUnixMs++ }, transport.LimitExceeded},
		{"timeout_zero", func(m *agentv1.TaskOffer) { m.TimeoutSeconds = 0 }, transport.InvalidField},
		{"timeout_limit", func(m *agentv1.TaskOffer) { m.TimeoutSeconds = 61 }, transport.LimitExceeded},
		{"plugin_limit", func(m *agentv1.TaskOffer) { m.PluginId = strings.Repeat("p", 257) }, transport.LimitExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sample := examples()[4]
			tc.mutate(sample.e.GetTaskOffer())
			wire, err := proto.Marshal(sample.e)
			if err != nil {
				t.Fatal(err)
			}
			if err := proto.Unmarshal(wire, sample.e); err != nil {
				t.Fatal(err)
			}
			assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, limits()), tc.code)
		})
	}
	sample := examples()[4]
	for _, actions := range []map[string]string{nil, {}, {"synthetic.read": "v2"}} {
		l := limits()
		l.AllowedActions = actions
		assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, l), transport.ActionNotAllowed)
	}
	sample.e.GetTaskOffer().Privilege = agentv1.Privilege_PRIVILEGE_ROOT
	if err := transport.ValidateEnvelope(sample.e, sample.ctx, limits()); err != nil {
		t.Fatal(err)
	} // Syntax only, not approval.
}

func TestExecutionMessageIDsAndEnums(t *testing.T) {
	for _, index := range []int{5, 6, 7, 8, 9} {
		sample := examples()[index]
		for _, name := range []string{"task_id", "execution_id"} {
			t.Run(sample.name+"/"+name, func(t *testing.T) {
				e := proto.Clone(sample.e).(*agentv1.Envelope)
				field := e.ProtoReflect().WhichOneof(e.ProtoReflect().Descriptor().Oneofs().ByName("message"))
				p := e.ProtoReflect().Mutable(field).Message()
				for i := 0; i < p.Descriptor().Fields().Len(); i++ {
					f := p.Descriptor().Fields().Get(i)
					if string(f.Name()) == name {
						p.Clear(f)
					}
				}
				assertCode(t, transport.ValidateEnvelope(e, sample.ctx, limits()), transport.MissingID)
			})
		}
	}
	for _, result := range []agentv1.ExecutionResult{0, 999, -1} {
		sample := examples()[8]
		sample.e.GetTaskFinished().Result = result
		wire, err := proto.Marshal(sample.e)
		if err != nil {
			t.Fatal(err)
		}
		if err := proto.Unmarshal(wire, sample.e); err != nil {
			t.Fatal(err)
		}
		assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, limits()), transport.UnknownEnum)
	}
	for _, result := range []agentv1.ExecutionResult{1, 2, 3, 4, 5} {
		sample := examples()[8]
		sample.e.GetTaskFinished().Result = result
		if err := transport.ValidateEnvelope(sample.e, sample.ctx, limits()); err != nil {
			t.Fatal(err)
		}
	}
	for _, stream := range []agentv1.TaskOutput_Stream{0, 999, -1} {
		sample := examples()[7]
		sample.e.GetTaskOutput().Stream = stream
		assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, limits()), transport.UnknownEnum)
	}
}

func TestOtherPayloadFieldRejections(t *testing.T) {
	cases := []struct {
		name   string
		index  int
		mutate func(*agentv1.Envelope)
		code   transport.ValidationCode
	}{
		{"sequence_zero", 2, func(e *agentv1.Envelope) { e.Sequence = 0 }, transport.InvalidField},
		{"sent_time_missing", 2, func(e *agentv1.Envelope) { e.SentAtUnixMs = 0 }, transport.InvalidField},
		{"text_limit", 2, func(e *agentv1.Envelope) { e.NodeId = strings.Repeat("s", 257) }, transport.LimitExceeded},
		{"invalid_utf8", 2, func(e *agentv1.Envelope) { e.NodeId = string([]byte{0xff}) }, transport.InvalidField},
		{"agent_boot_missing", 0, func(e *agentv1.Envelope) { e.GetAgentHello().BootId = "" }, transport.MissingID},
		{"agent_range_zero", 0, func(e *agentv1.Envelope) { e.GetAgentHello().MinProtocolMajor = 0 }, transport.InvalidProtocol},
		{"agent_range_reversed", 0, func(e *agentv1.Envelope) { e.GetAgentHello().MinProtocolMajor = 2 }, transport.InvalidProtocol},
		{"agent_range_excludes", 0, func(e *agentv1.Envelope) {
			e.GetAgentHello().MinProtocolMajor = 2
			e.GetAgentHello().MaxProtocolMajor = 3
		}, transport.InvalidProtocol},
		{"capability_duplicate", 0, func(e *agentv1.Envelope) { e.GetAgentHello().CapabilityIds = []string{"cap", "cap"} }, transport.InvalidField},
		{"capability_missing", 0, func(e *agentv1.Envelope) { e.GetAgentHello().CapabilityIds = []string{" "} }, transport.MissingID},
		{"capability_limit", 0, func(e *agentv1.Envelope) { e.GetAgentHello().CapabilityIds = []string{"a", "b", "c", "d", "e"} }, transport.LimitExceeded},
		{"selected_major", 1, func(e *agentv1.Envelope) { e.GetControllerHello().SelectedProtocolMajor = 0 }, transport.InvalidProtocol},
		{"selected_minor", 1, func(e *agentv1.Envelope) { e.GetControllerHello().SelectedProtocolMinor++ }, transport.InvalidProtocol},
		{"controller_heartbeat", 1, func(e *agentv1.Envelope) { e.GetControllerHello().HeartbeatIntervalSeconds = 0 }, transport.InvalidField},
		{"controller_frame_zero", 1, func(e *agentv1.Envelope) { e.GetControllerHello().MaxFrameBytes = 0 }, transport.InvalidField},
		{"controller_frame_limit", 1, func(e *agentv1.Envelope) { e.GetControllerHello().MaxFrameBytes++ }, transport.LimitExceeded},
		{"controller_time", 1, func(e *agentv1.Envelope) { e.GetControllerHello().ServerTimeUnixMs = 0 }, transport.InvalidField},
		{"controller_rejection_reason", 1, func(e *agentv1.Envelope) { e.GetControllerHello().Accepted = false }, transport.MissingID},
		{"heartbeat_status", 2, func(e *agentv1.Envelope) { e.GetHeartbeat().ConnectivityStatus = "" }, transport.MissingID},
		{"metrics_id", 3, func(e *agentv1.Envelope) { e.GetMetrics().BatchId = "" }, transport.MissingID},
		{"metrics_empty", 3, func(e *agentv1.Envelope) { e.GetMetrics().Points = nil }, transport.InvalidField},
		{"metrics_nil_point", 3, func(e *agentv1.Envelope) { e.GetMetrics().Points = []*agentv1.MetricPoint{nil} }, transport.InvalidField},
		{"metrics_name", 3, func(e *agentv1.Envelope) { e.GetMetrics().Points[0].Name = "" }, transport.MissingID},
		{"metrics_unit", 3, func(e *agentv1.Envelope) { e.GetMetrics().Points[0].Unit = "" }, transport.MissingID},
		{"metrics_nan", 3, func(e *agentv1.Envelope) { e.GetMetrics().Points[0].Value = math.NaN() }, transport.InvalidField},
		{"metrics_posinf", 3, func(e *agentv1.Envelope) { e.GetMetrics().Points[0].Value = math.Inf(1) }, transport.InvalidField},
		{"metrics_neginf", 3, func(e *agentv1.Envelope) { e.GetMetrics().Points[0].Value = math.Inf(-1) }, transport.InvalidField},
		{"metrics_time", 3, func(e *agentv1.Envelope) { e.GetMetrics().Points[0].ObservedAtUnixMs = 0 }, transport.InvalidField},
		{"ack_rejected_without_reason", 5, func(e *agentv1.Envelope) { e.GetTaskAck().Accepted = false }, transport.MissingID},
		{"started_time", 6, func(e *agentv1.Envelope) { e.GetTaskStarted().StartedAtUnixMs = 0 }, transport.InvalidField},
		{"output_sequence", 7, func(e *agentv1.Envelope) { e.GetTaskOutput().StreamSequence = 0 }, transport.InvalidField},
		{"output_empty", 7, func(e *agentv1.Envelope) { e.GetTaskOutput().Chunk = nil }, transport.InvalidField},
		{"output_limit", 7, func(e *agentv1.Envelope) { e.GetTaskOutput().Chunk = make([]byte, 1025) }, transport.LimitExceeded},
		{"finished_time", 8, func(e *agentv1.Envelope) { e.GetTaskFinished().FinishedAtUnixMs = 0 }, transport.InvalidField},
		{"cancel_time", 9, func(e *agentv1.Envelope) { e.GetTaskCancel().RequestedAtUnixMs = 0 }, transport.InvalidField},
		{"cancel_reason", 9, func(e *agentv1.Envelope) { e.GetTaskCancel().ReasonCode = "" }, transport.MissingID},
		{"sync_request_id", 10, func(e *agentv1.Envelope) { e.GetStateSyncRequest().SyncId = "" }, transport.MissingID},
		{"sync_request_zero", 10, func(e *agentv1.Envelope) { e.GetStateSyncRequest().MaxExecutions = 0 }, transport.InvalidField},
		{"sync_request_limit", 10, func(e *agentv1.Envelope) { e.GetStateSyncRequest().MaxExecutions = 5 }, transport.LimitExceeded},
		{"sync_request_time", 10, func(e *agentv1.Envelope) { e.GetStateSyncRequest().SinceUnixMs = -1 }, transport.InvalidField},
		{"sync_reply_id", 11, func(e *agentv1.Envelope) { e.GetStateSyncReply().SyncId = "" }, transport.MissingID},
		{"sync_nil", 11, func(e *agentv1.Envelope) { e.GetStateSyncReply().Executions = []*agentv1.ExecutionSnapshot{nil} }, transport.InvalidField},
		{"sync_task_id", 11, func(e *agentv1.Envelope) { e.GetStateSyncReply().Executions[0].TaskId = "" }, transport.MissingID},
		{"sync_execution_id", 11, func(e *agentv1.Envelope) { e.GetStateSyncReply().Executions[0].ExecutionId = "" }, transport.MissingID},
		{"sync_duplicate", 11, func(e *agentv1.Envelope) {
			r := e.GetStateSyncReply()
			r.Executions = append(r.Executions, proto.Clone(r.Executions[0]).(*agentv1.ExecutionSnapshot))
		}, transport.InvalidField},
		{"sync_enum_unknown", 11, func(e *agentv1.Envelope) { e.GetStateSyncReply().Executions[0].Result = 999 }, transport.UnknownEnum},
		{"sync_time", 11, func(e *agentv1.Envelope) { e.GetStateSyncReply().Executions[0].UpdatedAtUnixMs = 0 }, transport.InvalidField},
		{"sync_success_not_started", 11, func(e *agentv1.Envelope) { e.GetStateSyncReply().Executions[0].Started = false }, transport.InvalidField},
		{"control_code", 12, func(e *agentv1.Envelope) { e.GetControlError().Code = "" }, transport.MissingID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sample := examples()[tc.index]
			tc.mutate(sample.e)
			assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, limits()), tc.code)
		})
	}
}

func TestInjectedSizeLimitsAtBoundary(t *testing.T) {
	sample := examples()[7]
	l := limits()
	l.MaxMessageBytes = proto.Size(sample.e)
	if err := transport.ValidateEnvelope(sample.e, sample.ctx, l); err != nil {
		t.Fatal(err)
	}
	l.MaxMessageBytes--
	assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, l), transport.LimitExceeded)
	l = limits()
	sample.e.GetTaskOutput().Chunk = make([]byte, l.MaxOutputBytes)
	if err := transport.ValidateEnvelope(sample.e, sample.ctx, l); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []example{examples()[3], examples()[11]} {
		l := limits()
		if sample.name == "metrics" {
			p := sample.e.GetMetrics()
			p.Points = append(p.Points, proto.Clone(p.Points[0]).(*agentv1.MetricPoint))
			l.MaxMetrics = 1
		} else {
			p := sample.e.GetStateSyncReply()
			next := proto.Clone(p.Executions[0]).(*agentv1.ExecutionSnapshot)
			next.ExecutionId = "other"
			p.Executions = append(p.Executions, next)
			l.MaxSyncExecutions = 1
		}
		assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, l), transport.LimitExceeded)
	}
}

func TestOfferClockDoesNotExpireExecutionEvidence(t *testing.T) {
	for _, index := range []int{6, 7, 8, 9, 11} {
		sample := examples()[index]
		sample.ctx.Now = fixedNow.Add(365 * 24 * time.Hour)
		if err := transport.ValidateEnvelope(sample.e, sample.ctx, limits()); err != nil {
			t.Fatalf("late %s discarded: %v", sample.name, err)
		}
	}
	sample := examples()[4]
	sample.ctx.Now = fixedNow.Add(time.Hour)
	assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, limits()), transport.ExpiredOffer)
	sample = examples()[4]
	l := limits()
	l.MaxOfferLifetime = time.Second
	assertCode(t, transport.ValidateEnvelope(sample.e, sample.ctx, l), transport.LimitExceeded)
	sample.e.GetTaskOffer().ExpiresAtUnixMs = fixedNow.UnixMilli() + 1
	if err := transport.ValidateEnvelope(sample.e, sample.ctx, l); err != nil {
		t.Fatal(err)
	}
}

func TestNonterminalSnapshotsAndRejectionsRemainRepresentable(t *testing.T) {
	for _, started := range []bool{false, true} {
		sample := examples()[11]
		s := sample.e.GetStateSyncReply().Executions[0]
		s.Result = 0
		s.Started = started
		if err := transport.ValidateEnvelope(sample.e, sample.ctx, limits()); err != nil {
			t.Fatal(err)
		}
	}
	sample := examples()[11]
	sample.e.GetStateSyncReply().Executions = nil
	sample.e.GetStateSyncReply().Truncated = true
	if err := transport.ValidateEnvelope(sample.e, sample.ctx, limits()); err != nil {
		t.Fatal(err)
	} // Incomplete, not "none exist".
	sample = examples()[1]
	sample.e.GetControllerHello().Accepted = false
	sample.e.GetControllerHello().ReasonCode = "INCOMPATIBLE_PROTOCOL"
	if err := transport.ValidateEnvelope(sample.e, sample.ctx, limits()); err != nil {
		t.Fatal(err)
	} // Valid rejection, not acceptance.
	sample = examples()[5]
	sample.e.GetTaskAck().Accepted = false
	sample.e.GetTaskAck().ReasonCode = "ACTION_UNAVAILABLE"
	if err := transport.ValidateEnvelope(sample.e, sample.ctx, limits()); err != nil {
		t.Fatal(err)
	}
	sample = examples()[7]
	sample.e.GetTaskOutput().Chunk = nil
	sample.e.GetTaskOutput().Truncated = true
	sample.e.GetTaskOutput().Stream = agentv1.TaskOutput_STREAM_STDERR
	if err := transport.ValidateEnvelope(sample.e, sample.ctx, limits()); err != nil {
		t.Fatal(err)
	}
	sample = examples()[12]
	sample.ctx.Phase = transport.AwaitControllerHello
	if err := transport.ValidateEnvelope(sample.e, sample.ctx, limits()); err != nil {
		t.Fatal(err)
	}
}

func TestErrorsDoNotEchoUntrustedValues(t *testing.T) {
	sample := examples()[4]
	marker := "synthetic-secret-do-not-echo"
	sample.e.GetTaskOffer().ActionId = marker
	err := transport.ValidateEnvelope(sample.e, sample.ctx, limits())
	assertCode(t, err, transport.ActionNotAllowed)
	if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "synthetic-task") {
		t.Fatal("error leaked untrusted fields")
	}
}

func TestUnknownFieldsAreNotInferredAsMessagesOrAuthority(t *testing.T) {
	decoded := new(agentv1.Envelope)
	if err := proto.Unmarshal([]byte{0x8a, 0x02, 0x00}, decoded); err != nil {
		t.Fatal(err)
	} // Future oneof field33.
	assertCode(t, transport.ValidateEnvelope(decoded, contextFor(transport.AgentToController, transport.Established), limits()), transport.MissingPayload)
	sample := examples()[2]
	sample.e.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
	if err := transport.ValidateEnvelope(sample.e, sample.ctx, limits()); err != nil {
		t.Fatal(err)
	} // Opaque extensions, not permissions.
}
