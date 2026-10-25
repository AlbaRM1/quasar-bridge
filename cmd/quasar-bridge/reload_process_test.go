package main

import (
	"context"
	"encoding/json/v2"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServerSIGHUPReload(t *testing.T) {
	if os.Getenv("QUASAR_RELOAD_TEST_CHILD") == "1" {
		flag.CommandLine = flag.NewFlagSet("quasar-bridge", flag.ExitOnError)
		os.Args = []string{"quasar-bridge", "-config", os.Getenv("QUASAR_RELOAD_TEST_CONFIG")}
		main()
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("SIGHUP is a Unix reload mechanism")
	}
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	path := filepath.Join(t.TempDir(), "config.json")
	write := func(route string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"listen": address, "routing": map[string]any{"rules": []any{}, "default_route": route}})
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("yandex")
	logs, err := os.CreateTemp(t.TempDir(), "child-log")
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestServerSIGHUPReload$")
	child.Env = append(os.Environ(), "QUASAR_RELOAD_TEST_CHILD=1", "QUASAR_RELOAD_TEST_CONFIG="+path)
	child.Stdout = logs
	child.Stderr = logs
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Signal(syscall.SIGTERM); _ = child.Wait() })
	hc := &http.Client{Timeout: time.Second}
	revision := func() string {
		r, err := hc.Get("http://" + address + "/healthz")
		if err != nil {
			return ""
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		var body struct {
			Revision string `json:"routing_revision"`
		}
		_ = json.Unmarshal(b, &body)
		return body.Revision
	}
	wait := func(accept func(string) bool) string {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if r := revision(); accept(r) {
				return r
			}
			time.Sleep(10 * time.Millisecond)
		}
		_, _ = logs.Seek(0, 0)
		b, _ := io.ReadAll(logs)
		t.Fatalf("health/reload did not converge: %s", b)
		return ""
	}
	before := wait(func(r string) bool { return r != "" })
	write("llm")
	if err := child.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	after := wait(func(r string) bool { return r != "" && r != before })
	if err := os.WriteFile(path, []byte(`{"routing":{"rules":[{"name":"broken","route":"home","match":{"regex":"["}}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	// Synchronize with rejection in the child's log rather than assuming a delay.
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(logs.Name())
		if strings.Contains(string(b), "routing_reload_rejected") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("missing reload rejection")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := revision(); got != after {
		t.Fatalf("rejected policy changed live process: %s", got)
	}
}
