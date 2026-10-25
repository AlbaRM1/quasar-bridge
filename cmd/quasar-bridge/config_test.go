package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"quasarbridge/internal/bridge"
)

func configFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestReadConfigEnvironmentOverridesDeploymentSettings(t *testing.T) {
	t.Setenv("QUASAR_MODE", "hybrid")
	t.Setenv("QUASAR_LISTEN", "localhost:9443")
	t.Setenv("QUASAR_UPSTREAM", "wss://upstream.example.invalid/uni.ws")
	t.Setenv("QUASAR_TLS_CERT", "runtime/public.pem")
	t.Setenv("QUASAR_TLS_KEY", "runtime/private.key")
	t.Setenv("QUASAR_ALLOWED_IPS", "192.0.2.10, 2001:db8::10")
	t.Setenv("QUASAR_LLM_BASE_URL", "https://llm.example.invalid/v1")
	t.Setenv("QUASAR_LLM_MODEL", "configured-model")
	t.Setenv("QUASAR_HOME_MCP_URL", "https://home.example.invalid/mcp")
	t.Setenv("QUASAR_HOME_WARM_AREA", "Office")
	cfg, err := readConfig(configFile(t, `{"mode":"observe","llm_model":"previous"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "localhost:9443" || cfg.Upstream != "wss://upstream.example.invalid/uni.ws" || cfg.TLSCert != "runtime/public.pem" || cfg.TLSKey != "runtime/private.key" || cfg.Mode != "hybrid" || cfg.LLMModel != "configured-model" || cfg.HomeWarmArea != "Office" || len(cfg.AllowedIPs) != 2 {
		t.Fatalf("configuration override failed: %+v", cfg)
	}
}
func TestReadConfigRejectsInvalidAndUnknownSettings(t *testing.T) {
	for _, body := range []string{
		`{"unexpected":"value"}`, `{"mode":"unknown"}`, `{"tls_cert":"cert.pem"}`,
		`{"allowed_ips":["not-an-address"]}`, `{"upstream":"file:///tmp/socket"}`,
		`{"llm_base_url":"https://name:password@example.invalid/v1"}`,
		`{"mode":"hybrid"}`,
	} {
		t.Run(body, func(t *testing.T) {
			if _, err := readConfig(configFile(t, body)); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
func TestReadConfigHasGenericDefaults(t *testing.T) {
	cfg, err := readConfig(configFile(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "observe" || !strings.HasPrefix(cfg.Listen, "localhost:") {
		t.Fatalf("defaults=%+v", cfg)
	}
}

func TestRoutingDefaultsAndPartialOverlay(t *testing.T) {
	cfg, err := readConfig(configFile(t, `{"routing":{"classifier":{"on_error":"llm"},"refusal":{"enabled":false}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Routing.Rules) != 3 || cfg.Routing.Classifier.TimeoutMS != 8000 || cfg.Routing.Classifier.OnError != "llm" || cfg.Routing.Refusal.Enabled {
		t.Fatalf("partial defaults lost: %+v", cfg.Routing)
	}
	empty, err := readConfig(configFile(t, `{"routing":{"rules":[],"default_route":"yandex"}}`))
	if err != nil || len(empty.Routing.Rules) != 0 {
		t.Fatalf("empty rules: %v", err)
	}
	for _, body := range []string{
		`{"routing":{"default_route":"invalid"}}`,
		`{"routing":{"rules":[{"name":"bad","route":"llm","match":{"regex":"["}}]}}`,
		`{"routing":{"rules":[{"name":"bad","route":"llm","match":{"typo":["x"]}}]}}`,
	} {
		if _, err := readConfig(configFile(t, body)); err == nil {
			t.Fatal("invalid routing accepted")
		}
	}
}

func TestRuleArrayReplacesDefaultElements(t *testing.T) {
	cfg, err := readConfig(configFile(t, `{"routing":{"rules":[{"name":"custom","route":"llm","match":{"contains_any":["ping"]}}],"default_route":"yandex"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Routing.Rules) != 1 || len(cfg.Routing.Rules[0].Match.ContainsAllGroups) != 0 {
		t.Fatal("custom rule inherited built-in conditions")
	}
	e := &bridge.Engine{}
	if err := e.SetRouting(cfg.Routing); err != nil {
		t.Fatal(err)
	}
	if got := e.Decide(t.Context(), "PING"); got.Route != "llm" || got.Rule != "custom" {
		t.Fatal(got)
	}
}

func TestExampleRoutingEqualsBackwardCompatibleDefaults(t *testing.T) {
	t.Setenv("QUASAR_HOME_MCP_URL", "https://home.example.invalid/mcp")
	cfg, err := readConfig("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Routing, bridge.DefaultRoutingConfig()) {
		t.Fatal("example defaults drifted from legacy policy")
	}
}
