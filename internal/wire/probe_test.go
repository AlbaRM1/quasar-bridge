package wire

import "testing"

func TestProbeReportsJSONStructureWithoutValues(t *testing.T) {
	data := append([]byte{0, 0, 0, 1}, []byte(`{"header":{"authorization":"secret"},"payload":{"token":"secret"}}`)...)
	p := Probe(data, ClientToServer)
	if p.Offset != 4 || p.Format != "json" || len(p.Keys) != 2 {
		t.Fatalf("probe=%+v", p)
	}
}
