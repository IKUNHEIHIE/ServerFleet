package transport

import (
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

// MaxSupportedFrameDepth is an implementation safety ceiling, not a default or
// a frozen protocol parameter. Callers must explicitly choose a positive depth.
const MaxSupportedFrameDepth = 128

// FrameDecodeLimits bounds actual input bytes, total scanned tags (including
// nested known messages and group terminators), and nesting including the root.
// No zero values select library defaults.
type FrameDecodeLimits struct {
	MaxFrameBytes int
	MaxFields     int
	MaxDepth      int
}

type FrameErrorCode string

const (
	FrameInvalidLimits     FrameErrorCode = "FRAME_INVALID_LIMITS"
	FrameTooLarge          FrameErrorCode = "FRAME_TOO_LARGE"
	FrameMalformed         FrameErrorCode = "FRAME_MALFORMED"
	FrameFieldLimit        FrameErrorCode = "FRAME_FIELD_LIMIT"
	FrameDepthLimit        FrameErrorCode = "FRAME_DEPTH_LIMIT"
	FrameDuplicateMetadata FrameErrorCode = "FRAME_DUPLICATE_METADATA"
	FrameDuplicatePayload  FrameErrorCode = "FRAME_DUPLICATE_PAYLOAD"
	FrameDecodeFailed      FrameErrorCode = "FRAME_DECODE_FAILED"
)

// FrameError contains only a fixed category; decoder errors and wire data are
// never included. Semantic errors remain the existing *ValidationError type.
type FrameError struct {
	Code FrameErrorCode
}

func (e *FrameError) Error() string { return "protobuf frame: " + string(e.Code) }

func frameError(code FrameErrorCode) error { return &FrameError{Code: code} }

// DecodeEnvelopeFrame performs raw-size check -> bounded structural preflight ->
// bounded protobuf decode -> existing semantic validation. Every failure returns
// a nil message, even if the protobuf decoder constructed a partial message.
// Input and policies must remain immutable during this call. Success is not
// identity, authorization, replay protection, or permission to execute anything.
func DecodeEnvelopeFrame(raw []byte, ctx ValidationContext, semantic ValidationLimits, limits FrameDecodeLimits) (*agentv1.Envelope, error) {
	if limits.MaxFrameBytes <= 0 || limits.MaxFields <= 0 || limits.MaxDepth <= 0 || limits.MaxDepth > MaxSupportedFrameDepth {
		return nil, frameError(FrameInvalidLimits)
	}
	// Do not copy, scan, allocate a decoded message, or invoke protobuf first.
	if len(raw) > limits.MaxFrameBytes {
		return nil, frameError(FrameTooLarge)
	}
	if len(raw) == 0 {
		return nil, frameError(FrameMalformed)
	}
	descriptor := (&agentv1.Envelope{}).ProtoReflect().Descriptor()
	if err := scanEnvelopeWire(raw, descriptor, limits); err != nil {
		return nil, err
	}
	decoded := new(agentv1.Envelope)
	options := proto.UnmarshalOptions{RecursionLimit: limits.MaxDepth, DiscardUnknown: false, NoLazyDecoding: true}
	if err := options.Unmarshal(raw, decoded); err != nil {
		return nil, frameError(FrameDecodeFailed)
	}
	if err := ValidateEnvelope(decoded, ctx, semantic); err != nil {
		return nil, err
	}
	return decoded, nil
}

// wireScope holds slices into raw; it does not copy length-delimited contents.
// Unknown bytes are opaque. Unknown groups are scanned iteratively, never via
// protowire.ConsumeFieldValue (which recursively skips groups).
type wireScope struct {
	data       []byte
	descriptor protoreflect.MessageDescriptor
	endGroup   protowire.Number
}

func scanEnvelopeWire(raw []byte, descriptor protoreflect.MessageDescriptor, limits FrameDecodeLimits) error {
	stack := []wireScope{{data: raw, descriptor: descriptor}}
	fields := 0
	var metadata uint8
	payloadSeen := false
	for len(stack) > 0 {
		current := &stack[len(stack)-1]
		if len(current.data) == 0 {
			if current.endGroup != 0 {
				return frameError(FrameMalformed)
			}
			stack = stack[:len(stack)-1]
			continue
		}
		if fields == limits.MaxFields {
			return frameError(FrameFieldLimit)
		}
		fields++
		number, typ, n := protowire.ConsumeTag(current.data)
		if n < 0 || !number.IsValid() {
			return frameError(FrameMalformed)
		}
		current.data = current.data[n:]
		if typ == protowire.EndGroupType {
			if current.endGroup != number {
				return frameError(FrameMalformed)
			}
			tail := current.data
			stack = stack[:len(stack)-1]
			stack[len(stack)-1].data = tail // Matching group always has a parent.
			continue
		}
		var field protoreflect.FieldDescriptor
		if current.descriptor != nil {
			field = current.descriptor.Fields().ByNumber(protoreflect.FieldNumber(number))
		}
		if field != nil && typ != fieldWireType(field.Kind()) {
			return frameError(FrameMalformed)
		}
		if len(stack) == 1 {
			if number >= 1 && number <= 7 {
				bit := uint8(1 << (number - 1))
				if metadata&bit != 0 {
					return frameError(FrameDuplicateMetadata)
				}
				metadata |= bit
			}
			if number >= 20 && number <= 32 {
				if payloadSeen {
					return frameError(FrameDuplicatePayload)
				}
				payloadSeen = true
			}
		}
		var child *wireScope
		switch typ {
		case protowire.VarintType:
			_, n = protowire.ConsumeVarint(current.data)
		case protowire.Fixed32Type:
			_, n = protowire.ConsumeFixed32(current.data)
		case protowire.Fixed64Type:
			_, n = protowire.ConsumeFixed64(current.data)
		case protowire.BytesType:
			var body []byte
			body, n = protowire.ConsumeBytes(current.data)
			if n >= 0 && field != nil && field.Kind() == protoreflect.MessageKind {
				child = &wireScope{data: body, descriptor: field.Message()}
			}
		case protowire.StartGroupType:
			child = &wireScope{data: current.data, endGroup: number}
			if field != nil {
				child.descriptor = field.Message()
			}
			n = 0 // The child consumes through the matching end marker.
		default:
			return frameError(FrameMalformed)
		}
		if n < 0 {
			return frameError(FrameMalformed)
		}
		current.data = current.data[n:]
		if child != nil {
			if len(stack) == limits.MaxDepth {
				return frameError(FrameDepthLimit)
			}
			stack = append(stack, *child)
		}
	}
	return nil
}

// Only descriptor-known message fields are descended into. In particular,
// arbitrary future bytes/string extensions must not be parsed as messages.
// Current candidate has no packed numeric repeated fields; a schema change
// adding those requires reviewing this strict known-field wire-type policy.
func fieldWireType(kind protoreflect.Kind) protowire.Type {
	switch kind {
	case protoreflect.DoubleKind, protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind:
		return protowire.Fixed64Type
	case protoreflect.FloatKind, protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind:
		return protowire.Fixed32Type
	case protoreflect.StringKind, protoreflect.BytesKind, protoreflect.MessageKind:
		return protowire.BytesType
	case protoreflect.GroupKind:
		return protowire.StartGroupType
	default:
		return protowire.VarintType
	}
}
