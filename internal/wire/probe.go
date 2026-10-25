package wire

import (
	"encoding/binary"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
)

// Probe reports framing structure only, never field values or raw payloads.
type ProbeResult struct {
	Offset    int
	Format    string
	Kind      string
	Keys      []string
	FirstByte int
	SmallLE   int
	SmallBE   int
}

func Probe(data []byte, d Direction) ProbeResult {
	r := ProbeResult{Format: "opaque", SmallLE: -1, SmallBE: -1}
	if len(data) == 0 {
		return r
	}
	r.FirstByte = int(data[0])
	if len(data) >= 4 {
		if n := binary.LittleEndian.Uint32(data); n < 65536 {
			r.SmallLE = int(n)
		}
		if n := binary.BigEndian.Uint32(data); n < 65536 {
			r.SmallBE = int(n)
		}
	}
	for offset := 0; offset < min(len(data), 33); offset++ {
		if f := Decode(data[offset:], d); f.Kind() != "opaque" {
			r.Offset = offset
			r.Format = "protobuf"
			r.Kind = f.Kind()
			return r
		}
		var obj map[string]jsontext.Value
		if json.Unmarshal(data[offset:], &obj) == nil && obj != nil {
			r.Offset = offset
			r.Format = "json"
			for key := range obj {
				if len(key) <= 40 {
					r.Keys = append(r.Keys, key)
				}
			}
			r.Keys = slices.Sorted(slices.Values(r.Keys))
			return r
		}
	}
	return r
}
