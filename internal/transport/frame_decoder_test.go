package transport_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

func frameLimits() transport.FrameDecodeLimits {
	return transport.FrameDecodeLimits{MaxFrameBytes: 32768, MaxFields: 256, MaxDepth: 8}
}

func encodeFrame(t testing.TB, e *agentv1.Envelope) []byte {
	t.Helper()
	wire, err := proto.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func varintField(number protowire.Number, value uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, number, protowire.VarintType), value)
}

func bytesField(number protowire.Number, value []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, number, protowire.BytesType), value)
}

func withoutPayload(t testing.TB, sample example) []byte {
	t.Helper()
	e := proto.Clone(sample.e).(*agentv1.Envelope)
	e.Message = nil
	return encodeFrame(t, e)
}

func groupWire(depth int) []byte {
	var out []byte
	for i := 0; i < depth; i++ {
		out = protowire.AppendTag(out, 100, protowire.StartGroupType)
	}
	out = append(out, varintField(101, 1)...)
	for i := 0; i < depth; i++ {
		out = protowire.AppendTag(out, 100, protowire.EndGroupType)
	}
	return out
}

func assertFrameFailure(t testing.TB, raw []byte, sample example, bounds transport.FrameDecodeLimits, code transport.FrameErrorCode) {
	t.Helper()
	before := bytes.Clone(raw)
	decoded, err := transport.DecodeEnvelopeFrame(raw, sample.ctx, limits(), bounds)
	var detail *transport.FrameError
	if decoded != nil || !errors.As(err, &detail) || detail.Code != code {
		t.Fatalf("got message=%v error=%v, want nil / %s", decoded, err, code)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("failed decode mutated input")
	}
}

func assertFrameSuccess(t testing.TB, raw []byte, sample example, bounds transport.FrameDecodeLimits) *agentv1.Envelope {
	t.Helper()
	before := bytes.Clone(raw)
	decoded, err := transport.DecodeEnvelopeFrame(raw, sample.ctx, limits(), bounds)
	if err != nil || decoded == nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("successful decode mutated input")
	}
	return decoded
}

func TestFrameDecodeAllKnownMessages(t *testing.T) {
	for _, sample := range examples() {
		t.Run(sample.name, func(t *testing.T) {
			decoded := assertFrameSuccess(t, encodeFrame(t, sample.e), sample, frameLimits())
			if !proto.Equal(decoded, sample.e) {
				t.Fatal("message changed")
			}
		})
	}
}

// The decoder-only/semantic probe at the task baseline accepted this exact raw
// duplicate. This regression now invokes the production raw-frame entry point.
func TestRawDuplicateMetadataRejected(t *testing.T) {
	sample := examples()[2]
	wire := encodeFrame(t, sample.e)
	wire = append(wire, 0x08, 0x01) // Repeat known Envelope.protocol_major=1.
	assertFrameFailure(t, wire, sample, frameLimits(), transport.FrameDuplicateMetadata)
}

func TestFrameDuplicateEveryMetadataField(t *testing.T) {
	sample := examples()[2]
	for number := protowire.Number(1); number <= 7; number++ {
		t.Run(fmt.Sprint(number), func(t *testing.T) {
			extra := varintField(number, 1)
			if number >= 3 && number <= 5 {
				extra = bytesField(number, []byte("synthetic-duplicate"))
			}
			raw := append(encodeFrame(t, sample.e), extra...)
			assertFrameFailure(t, raw, sample, frameLimits(), transport.FrameDuplicateMetadata)
		})
	}
}

func TestFrameDuplicateOneofAndCompleteConcatenation(t *testing.T) {
	for _, sample := range examples() {
		t.Run("same/"+sample.name, func(t *testing.T) {
			message := sample.e.ProtoReflect()
			field := message.WhichOneof(message.Descriptor().Oneofs().ByName("message"))
			body, err := proto.Marshal(message.Get(field).Message().Interface())
			if err != nil {
				t.Fatal(err)
			}
			raw := append(encodeFrame(t, sample.e), bytesField(protowire.Number(field.Number()), body)...)
			assertFrameFailure(t, raw, sample, frameLimits(), transport.FrameDuplicatePayload)
		})
	}
	sample := examples()[2]
	other := examples()[8].e.GetTaskFinished()
	body, err := proto.Marshal(other)
	if err != nil {
		t.Fatal(err)
	}
	assertFrameFailure(t, append(encodeFrame(t, sample.e), bytesField(28, body)...), sample, frameLimits(), transport.FrameDuplicatePayload)
	for _, second := range []example{examples()[2], examples()[7]} {
		raw := append(encodeFrame(t, sample.e), encodeFrame(t, second.e)...)
		assertFrameFailure(t, raw, sample, frameLimits(), transport.FrameDuplicateMetadata)
	}
}

func TestFrameActualSizeBeforeScanOrDecode(t *testing.T) {
	sample := examples()[2]
	raw := encodeFrame(t, sample.e)
	bounds := frameLimits()
	bounds.MaxFrameBytes = len(raw)
	assertFrameSuccess(t, raw, sample, bounds)
	bounds.MaxFrameBytes--
	assertFrameFailure(t, raw, sample, bounds, transport.FrameTooLarge)
	// Even malformed input must be rejected by actual length before wire parsing.
	assertFrameFailure(t, bytes.Repeat([]byte{0}, len(raw)), sample, bounds, transport.FrameTooLarge)
	raw = append(encodeFrame(t, sample.e), bytesField(100, bytes.Repeat([]byte{0xff}, 1024))...)
	bounds.MaxFrameBytes = len(raw) - 1
	assertFrameFailure(t, raw, sample, bounds, transport.FrameTooLarge)
	// Nonminimal known varint plus a legal unknown extension: re-encoding shrinks
	// the decoded message, so proto.Size alone is not the raw-frame budget.
	minimal := encodeFrame(t, sample.e)
	if !bytes.HasPrefix(minimal, []byte{8, 1}) {
		t.Fatal("review fixture wire order")
	}
	raw = append([]byte{8, 0x81, 0x00}, minimal[2:]...)
	raw = append(raw, varintField(100, 1)...)
	decoded := new(agentv1.Envelope)
	if err := proto.Unmarshal(raw, decoded); err != nil {
		t.Fatal(err)
	}
	if len(raw) <= proto.Size(decoded) {
		t.Fatal("fixture did not exercise raw/encoded size difference")
	}
	if err := transport.ValidateEnvelope(decoded, sample.ctx, limits()); err != nil {
		t.Fatal(err)
	}
	bounds.MaxFrameBytes = proto.Size(decoded)
	assertFrameFailure(t, raw, sample, bounds, transport.FrameTooLarge)
}

func TestFrameInvalidLimitsAndTypedNilBytes(t *testing.T) {
	sample := examples()[2]
	raw := encodeFrame(t, sample.e)
	for _, change := range []func(*transport.FrameDecodeLimits){
		func(l *transport.FrameDecodeLimits) { l.MaxFrameBytes = 0 }, func(l *transport.FrameDecodeLimits) { l.MaxFrameBytes = -1 },
		func(l *transport.FrameDecodeLimits) { l.MaxFields = 0 }, func(l *transport.FrameDecodeLimits) { l.MaxFields = -1 },
		func(l *transport.FrameDecodeLimits) { l.MaxDepth = 0 }, func(l *transport.FrameDecodeLimits) { l.MaxDepth = -1 },
		func(l *transport.FrameDecodeLimits) { l.MaxDepth = transport.MaxSupportedFrameDepth + 1 },
	} {
		bounds := frameLimits()
		change(&bounds)
		assertFrameFailure(t, raw, sample, bounds, transport.FrameInvalidLimits)
	}
	var typedNil []byte
	for _, input := range [][]byte{nil, {}, typedNil} {
		assertFrameFailure(t, input, sample, frameLimits(), transport.FrameMalformed)
	}
	// The typed-nil decoded-message behavior remains the existing validator's job.
	e := envelope()
	e.Message = (*agentv1.Envelope_TaskOffer)(nil)
	assertCode(t, transport.ValidateEnvelope(e, sample.ctx, limits()), transport.MissingPayload)
}

func TestFrameUnknownExtensionsPreservedAndOpaque(t *testing.T) {
	sample := examples()[2]
	unknown := varintField(100, 1)
	unknown = append(unknown, varintField(100, 2)...)
	unknown = protowire.AppendFixed64(protowire.AppendTag(unknown, 101, protowire.Fixed64Type), 123)
	unknown = append(unknown, bytesField(102, []byte{0, 0xff, 0x80})...)
	unknown = append(unknown, groupWire(2)...)
	unknown = protowire.AppendFixed32(protowire.AppendTag(unknown, 103, protowire.Fixed32Type), 456)
	raw := append(encodeFrame(t, sample.e), unknown...)
	decoded := assertFrameSuccess(t, raw, sample, frameLimits())
	if !bytes.Equal(decoded.ProtoReflect().GetUnknown(), unknown) {
		t.Fatal("unknown extensions lost")
	}
	before := proto.Clone(decoded)
	for i := range raw {
		raw[i] = 0
	}
	if !proto.Equal(decoded, before) {
		t.Fatal("decoded message aliases input")
	}
	// Unknown bytes are not recursively interpreted as protobuf, even when they
	// resemble many fields. Their outer tag still consumes the work budget.
	unknown = bytesField(100, bytes.Repeat([]byte{8, 1}, 100))
	bounds := frameLimits()
	bounds.MaxFields = 10 // heartbeat has 9 known tags.
	assertFrameSuccess(t, append(encodeFrame(t, sample.e), unknown...), sample, bounds)
	// Field 1 / oneof-like tag 20 inside an UNKNOWN group is not Envelope scope.
	unknown = protowire.AppendTag(nil, 100, protowire.StartGroupType)
	unknown = append(unknown, varintField(1, 1)...)
	unknown = append(unknown, bytesField(20, nil)...)
	unknown = protowire.AppendTag(unknown, 100, protowire.EndGroupType)
	assertFrameSuccess(t, append(encodeFrame(t, sample.e), unknown...), sample, frameLimits())
	// A future payload field alongside a known payload stays an opaque extension.
	assertFrameSuccess(t, append(encodeFrame(t, sample.e), bytesField(100, []byte{0xff})...), sample, frameLimits())
	// Extensions inside a known payload also remain compatible.
	body, err := proto.Marshal(sample.e.GetHeartbeat())
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, bytesField(100, []byte{0xff})...)
	raw = append(withoutPayload(t, sample), bytesField(22, body)...)
	decoded = assertFrameSuccess(t, raw, sample, frameLimits())
	if !bytes.Equal(decoded.GetHeartbeat().ProtoReflect().GetUnknown(), bytesField(100, []byte{0xff})) {
		t.Fatal("nested unknown lost")
	}
}

func TestFrameRepeatedPayloadFieldsRemainValid(t *testing.T) {
	sample := examples()[0]
	sample.e.GetAgentHello().CapabilityIds = append(sample.e.GetAgentHello().CapabilityIds, "synthetic.other")
	assertFrameSuccess(t, encodeFrame(t, sample.e), sample, frameLimits())
	sample = examples()[3]
	sample.e.GetMetrics().Points = append(sample.e.GetMetrics().Points, proto.Clone(sample.e.GetMetrics().Points[0]).(*agentv1.MetricPoint))
	assertFrameSuccess(t, encodeFrame(t, sample.e), sample, frameLimits())
	sample = examples()[11]
	next := proto.Clone(sample.e.GetStateSyncReply().Executions[0]).(*agentv1.ExecutionSnapshot)
	next.ExecutionId = "synthetic-second"
	sample.e.GetStateSyncReply().Executions = append(sample.e.GetStateSyncReply().Executions, next)
	assertFrameSuccess(t, encodeFrame(t, sample.e), sample, frameLimits())
	// The new duplicate policy is limited to Envelope, not payload scalars.
	sample = examples()[2]
	body, err := proto.Marshal(sample.e.GetHeartbeat())
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, bytesField(4, []byte("online"))...)
	assertFrameSuccess(t, append(withoutPayload(t, sample), bytesField(22, body)...), sample, frameLimits())
}

func TestFrameWorkAndDepthBoundaries(t *testing.T) {
	sample := examples()[2]
	raw := encodeFrame(t, sample.e)
	bounds := frameLimits()
	bounds.MaxFields = 9 // 7 metadata + oneof + status.
	assertFrameSuccess(t, raw, sample, bounds)
	bounds.MaxFields = 8
	assertFrameFailure(t, raw, sample, bounds, transport.FrameFieldLimit)
	bounds = frameLimits()
	bounds.MaxFields = 12 // Above plus group start/value/end.
	raw = append(raw, groupWire(1)...)
	assertFrameSuccess(t, raw, sample, bounds)
	bounds.MaxFields--
	assertFrameFailure(t, raw, sample, bounds, transport.FrameFieldLimit)
	bounds = frameLimits()
	bounds.MaxDepth = 2
	assertFrameSuccess(t, encodeFrame(t, sample.e), sample, bounds)
	bounds.MaxDepth = 1
	assertFrameFailure(t, encodeFrame(t, sample.e), sample, bounds, transport.FrameDepthLimit)
	sample = examples()[3]
	bounds.MaxDepth = 3
	assertFrameSuccess(t, encodeFrame(t, sample.e), sample, bounds)
	bounds.MaxDepth = 2
	assertFrameFailure(t, encodeFrame(t, sample.e), sample, bounds, transport.FrameDepthLimit)
	sample = examples()[2]
	bounds = frameLimits()
	bounds.MaxDepth = 3
	assertFrameSuccess(t, append(encodeFrame(t, sample.e), groupWire(2)...), sample, bounds)
	assertFrameFailure(t, append(encodeFrame(t, sample.e), groupWire(3)...), sample, bounds, transport.FrameDepthLimit)
	bounds.MaxDepth = transport.MaxSupportedFrameDepth
	bounds.MaxFields = 512
	assertFrameSuccess(t, append(encodeFrame(t, sample.e), groupWire(transport.MaxSupportedFrameDepth-1)...), sample, bounds)
	assertFrameFailure(t, append(encodeFrame(t, sample.e), groupWire(4096)...), sample, bounds, transport.FrameDepthLimit)
}

func TestFrameMalformedWireAndNoPartialMessage(t *testing.T) {
	sample := examples()[2]
	cases := map[string][]byte{
		"zero_tag": {0}, "truncated_tag": {0x80}, "overflow_tag": bytes.Repeat([]byte{0xff}, 10),
		"out_of_range_tag": protowire.AppendVarint(nil, uint64(protowire.MaxValidNumber+1)<<3),
		"reserved_type6":   protowire.AppendTag(nil, 100, 6), "reserved_type7": protowire.AppendTag(nil, 100, 7),
		"truncated_varint":        append(protowire.AppendTag(nil, 100, protowire.VarintType), 0x80),
		"overflow_varint":         append(protowire.AppendTag(nil, 100, protowire.VarintType), bytes.Repeat([]byte{0xff}, 10)...),
		"short_fixed32":           append(protowire.AppendTag(nil, 100, protowire.Fixed32Type), 1, 2, 3),
		"short_fixed64":           append(protowire.AppendTag(nil, 100, protowire.Fixed64Type), make([]byte, 7)...),
		"truncated_length":        append(protowire.AppendTag(nil, 100, protowire.BytesType), 0x80),
		"length_exceeds_body":     append(protowire.AppendTag(nil, 100, protowire.BytesType), 9, 1),
		"length_uint64_max":       protowire.AppendVarint(protowire.AppendTag(nil, 100, protowire.BytesType), ^uint64(0)),
		"unexpected_end_group":    protowire.AppendTag(nil, 100, protowire.EndGroupType),
		"missing_end_group":       protowire.AppendTag(nil, 100, protowire.StartGroupType),
		"mismatched_end_group":    protowire.AppendTag(protowire.AppendTag(nil, 100, protowire.StartGroupType), 101, protowire.EndGroupType),
		"wrong_known_scalar_type": bytesField(1, []byte{1}), "wrong_known_payload_type": varintField(22, 1),
		"malformed_known_child": bytesField(22, []byte{0}),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) { assertFrameFailure(t, bad, sample, frameLimits(), transport.FrameMalformed) })
	}
	// A valid known message prefix must not escape on trailing structural failure.
	assertFrameFailure(t, append(encodeFrame(t, sample.e), 0x80), sample, frameLimits(), transport.FrameMalformed)
	// Structurally sized string with invalid UTF-8 reaches protobuf decode, which
	// can allocate a partial Envelope; the API still returns nil.
	raw := append(withoutPayload(t, sample), bytesField(22, bytesField(4, []byte{0xff}))...)
	assertFrameFailure(t, raw, sample, frameLimits(), transport.FrameDecodeFailed)
}

func TestFrameSemanticFailureReturnsNoMessage(t *testing.T) {
	for _, tc := range []struct {
		sample example
		code   transport.ValidationCode
	}{
		{examples()[4], transport.UnknownEnum}, {examples()[2], transport.InvalidDirection},
		{examples()[2], transport.InvalidContext}, {examples()[2], transport.MissingID},
	} {
		sample := tc.sample
		switch tc.code {
		case transport.UnknownEnum:
			sample.e.GetTaskOffer().Privilege = 999
		case transport.InvalidDirection:
			sample.ctx.Direction = transport.ControllerToAgent
		case transport.InvalidContext:
			sample.ctx = transport.ValidationContext{}
		case transport.MissingID:
			sample.e.MessageId = ""
		}
		raw := encodeFrame(t, sample.e)
		before := bytes.Clone(raw)
		decoded, err := transport.DecodeEnvelopeFrame(raw, sample.ctx, limits(), frameLimits())
		if decoded != nil {
			t.Fatal("semantic failure exposed message")
		}
		assertCode(t, err, tc.code)
		if !bytes.Equal(raw, before) {
			t.Fatal("semantic failure mutated input")
		}
	}
	sample := examples()[2]
	raw := append(withoutPayload(t, sample), bytesField(100, nil)...)
	decoded, err := transport.DecodeEnvelopeFrame(raw, sample.ctx, limits(), frameLimits())
	if decoded != nil {
		t.Fatal("unknown payload-only exposed message")
	}
	assertCode(t, err, transport.MissingPayload)
}

func TestFrameErrorsDoNotEchoWireData(t *testing.T) {
	sample := examples()[2]
	marker := "synthetic-secret-do-not-echo"
	sample.e.MessageId = marker
	inputs := [][]byte{
		append(encodeFrame(t, sample.e), bytesField(3, []byte(marker))...),
		append(encodeFrame(t, sample.e), 0x80),
		append(withoutPayload(t, sample), bytesField(22, bytesField(4, append([]byte(marker), 0xff)))...),
	}
	for _, raw := range inputs {
		decoded, err := transport.DecodeEnvelopeFrame(raw, sample.ctx, limits(), frameLimits())
		if decoded != nil || err == nil || strings.Contains(err.Error(), marker) {
			t.Fatalf("unsafe result: %v %v", decoded, err)
		}
	}
}

func TestDisjointPartialEncodingHasNoIntrinsicMessageBoundary(t *testing.T) {
	sample := examples()[2]
	body, err := proto.Marshal(sample.e.GetHeartbeat())
	if err != nil {
		t.Fatal(err)
	}
	// Disjoint partial encodings concatenate into the same valid single Envelope.
	// This is deliberately NOT advertised as detectable complete-frame smuggling.
	raw := append(withoutPayload(t, sample), bytesField(22, body)...)
	decoded := assertFrameSuccess(t, raw, sample, frameLimits())
	if !proto.Equal(decoded, sample.e) {
		t.Fatal("valid composed encoding changed")
	}
}

func FuzzDecodeEnvelopeFrame(f *testing.F) {
	f.Add([]byte{0})
	f.Add(groupWire(20))
	for _, sample := range examples() {
		f.Add(encodeFrame(f, sample.e))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		before := bytes.Clone(raw)
		decoded, err := transport.DecodeEnvelopeFrame(raw, contextFor(transport.AgentToController, transport.Established), limits(), transport.FrameDecodeLimits{MaxFrameBytes: 1024, MaxFields: 64, MaxDepth: 8})
		if err != nil && decoded != nil {
			t.Fatal("failure exposed partial message")
		}
		if err == nil && decoded == nil {
			t.Fatal("success without message")
		}
		if !bytes.Equal(raw, before) {
			t.Fatal("input mutated")
		}
	})
}
