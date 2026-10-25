package main

import (
	"os"
	"testing"

	"quasarbridge/internal/bridge"
)

func TestReloadRoutingKeepsLastGoodSnapshot(t *testing.T) {
	path := configFile(t, `{"routing":{"rules":[],"default_route":"yandex"}}`)
	initial, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	engine := &bridge.Engine{}
	if err := engine.SetRouting(initial.Routing); err != nil {
		t.Fatal(err)
	}
	original := engine.RoutingStatus().Revision
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"routing":{"rules":[],"default_route":"llm"}}`)
	if err := reloadRouting(path, initial, engine); err != nil {
		t.Fatal(err)
	}
	good := engine.RoutingStatus().Revision
	if good == original || engine.Decide(t.Context(), "anything").Route != "llm" {
		t.Fatal("policy not installed")
	}
	for _, bad := range []string{
		`{"routing":{"rules":[],"default_route":"invalid"}}`,
		`{"routing":{"rules":[{"name":"bad","route":"home","match":{"regex":"["}}]}}`,
		`{"listen":"localhost:9999","routing":{"rules":[],"default_route":"home"}}`,
		`{"routing":`,
	} {
		write(bad)
		if err := reloadRouting(path, initial, engine); err == nil {
			t.Fatal("bad reload accepted")
		}
		if engine.RoutingStatus().Revision != good {
			t.Fatal("bad reload changed policy")
		}
	}
}
