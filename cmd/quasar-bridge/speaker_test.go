package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpeakerEndpointPreservesOtherConfigAndRollback(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "quasar.cfg")
	backup := filepath.Join(dir, "backup.json")
	original := `{"aliced":{"uniProxyUrl":"wss://previous.example.invalid/uni.ws","uniProxyQuicUrl":"quic://previous.example.invalid","other":3},"unknown":{"keep":true}}`
	if err := os.WriteFile(cfg, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	local := "ws://127.0.0.1:8443/uni.ws"
	if err := setSpeakerEndpoint(cfg, backup, local, "apply"); err != nil {
		t.Fatal(err)
	}
	if err := setSpeakerEndpoint(cfg, backup, local, "apply"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), local) || !strings.Contains(string(data), `"keep":true`) {
		t.Fatal("bad patch")
	}
	// An unrelated firmware/config update must survive rollback.
	data = []byte(strings.Replace(string(data), `"other":3`, `"other":4`, 1))
	os.WriteFile(cfg, data, 0644)
	if err := setSpeakerEndpoint(cfg, backup, local, "rollback"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(cfg)
	if !strings.Contains(string(data), "previous.example.invalid") || !strings.Contains(string(data), `"other":4`) {
		t.Fatal("bad rollback")
	}
}

func TestSpeakerEndpointRejectsExternalQUICChange(t *testing.T) {
	for _, action := range []string{"apply", "rollback"} {
		t.Run(action, func(t *testing.T) {
			dir := t.TempDir()
			cfg := filepath.Join(dir, "quasar.cfg")
			backup := filepath.Join(dir, "backup.json")
			local := "ws://127.0.0.1:8443/uni.ws"
			original := `{"aliced":{"uniProxyUrl":"wss://previous.example.invalid/uni.ws","uniProxyQuicUrl":"quic://previous.example.invalid"}}`
			if err := os.WriteFile(cfg, []byte(original), 0644); err != nil {
				t.Fatal(err)
			}
			if err := setSpeakerEndpoint(cfg, backup, local, "apply"); err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(cfg)
			raw = []byte(strings.Replace(string(raw), `"uniProxyQuicUrl":""`, `"uniProxyQuicUrl":"quic://changed.example.invalid"`, 1))
			os.WriteFile(cfg, raw, 0644)
			if setSpeakerEndpoint(cfg, backup, local, action) == nil {
				t.Fatal("external QUIC change overwritten")
			}
			after, _ := os.ReadFile(cfg)
			if string(raw) != string(after) {
				t.Fatal("config mutated on rejection")
			}
		})
	}
}
