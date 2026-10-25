package wire

import (
	"errors"

	"google.golang.org/protobuf/encoding/protowire"
)

// EnableApproval changes only the approval leaf; all unrelated wire bytes,
// including duplicate fields and unknown extensions, remain in their original form.
func EnableApproval(f Frame) ([]byte, error) {
	var field protowire.Number
	switch f.Kind() {
	case "TextInput":
		field = 2
	case "VoiceInput":
		field = 3
	case "SpeechInput":
		field = 18
	default:
		return nil, errors.New("not an Alice input")
	}
	prefix := len(f.Prefix)
	body, err := patchVarint(f.Raw[prefix:], []protowire.Number{1, field, 3, 17, 4}, 1)
	if err != nil {
		return nil, err
	}
	return append(append([]byte{}, f.Prefix...), body...), nil
}
func patchVarint(data []byte, path []protowire.Number, value uint64) ([]byte, error) {
	if len(path) == 1 {
		return protowire.AppendVarint(protowire.AppendTag(append([]byte{}, data...), path[0], protowire.VarintType), value), nil
	}
	var out []byte
	found := false
	for len(data) > 0 {
		num, kind, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, errors.New("invalid protobuf tag")
		}
		size := protowire.ConsumeFieldValue(num, kind, data[n:])
		if size < 0 {
			return nil, errors.New("invalid protobuf value")
		}
		if num == path[0] && kind == protowire.BytesType {
			body, consumed := protowire.ConsumeBytes(data[n:])
			if consumed < 0 {
				return nil, errors.New("invalid protobuf message")
			}
			patched, err := patchVarint(body, path[1:], value)
			if err != nil {
				return nil, err
			}
			out = protowire.AppendBytes(protowire.AppendTag(out, num, kind), patched)
			found = true
		} else {
			out = append(out, data[:n+size]...)
		}
		data = data[n+size:]
	}
	if !found {
		body, err := patchVarint(nil, path[1:], value)
		if err != nil {
			return nil, err
		}
		out = protowire.AppendBytes(protowire.AppendTag(out, path[0], protowire.BytesType), body)
	}
	return out, nil
}
