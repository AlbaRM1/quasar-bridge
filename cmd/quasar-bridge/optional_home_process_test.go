package main

import (
	"context"
	"encoding/json/v2"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestHybridWithoutHomeProcess(t *testing.T) {
	if action := os.Getenv("QUASAR_NO_HOME_CHILD"); action != "" {
		flag.CommandLine = flag.NewFlagSet("quasar-bridge", flag.ExitOnError)
		os.Args = []string{"quasar-bridge", "-config", os.Getenv("QUASAR_NO_HOME_CONFIG")}
		if action != "server" {
			os.Args = append(os.Args, "-"+action)
			if action == "route" {
				os.Args = append(os.Args, "выключи свет")
			} else if action == "query" {
				os.Args = append(os.Args, "спроси нейросеть: привет")
			}
		}
		main()
		return
	}
	var calls atomic.Int64
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Ответ модели"}}]}`))
	}))
	defer api.Close()
	for _, tc := range []struct {
		action, want string
		calls        int64
	}{
		{"check-config", "configuration is valid", 0},
		{"check-backends", "LLM: ok; Home Assistant: disabled", 1},
		{"route", `"route":"yandex"`, 0},
		{"query", "Ответ модели", 1},
		{"server", "", 0},
	} {
		t.Run(tc.action, func(t *testing.T) {
			listener, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			listener.Close()
			body, err := json.Marshal(map[string]any{"mode": "hybrid", "listen": address, "llm_base_url": api.URL + "/v1", "llm_model": "fixture", "home_warm_area": "Office"})
			if err != nil {
				t.Fatal(err)
			}
			path := configFile(t, string(body))
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHybridWithoutHomeProcess$")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "QUASAR_") {
					child.Env = append(child.Env, entry)
				}
			}
			child.Env = append(child.Env, "QUASAR_HOME_MCP_URL=", "QUASAR_NO_HOME_CHILD="+tc.action, "QUASAR_NO_HOME_CONFIG="+path)
			before := calls.Load()
			if tc.action != "server" {
				output, err := child.CombinedOutput()
				if err != nil || !strings.Contains(string(output), tc.want) {
					t.Fatalf("diagnostic failed: %v\n%s", err, output)
				}
			} else {
				if err := child.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = child.Process.Signal(syscall.SIGTERM); _ = child.Wait() }()
				hc := &http.Client{Timeout: time.Second}
				for {
					r, err := hc.Get("http://" + address + "/healthz")
					if err == nil {
						var state struct {
							Mode string `json:"mode"`
							Home *bool  `json:"home_assistant_enabled"`
						}
						decodeErr := json.UnmarshalRead(r.Body, &state)
						r.Body.Close()
						if decodeErr != nil || state.Mode != "hybrid" || state.Home == nil || *state.Home {
							t.Fatalf("wrong backend state: %+v %v", state, decodeErr)
						}
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("hybrid server did not start without HA")
					case <-time.After(10 * time.Millisecond):
					}
				}
			}
			if got := calls.Load() - before; got != tc.calls {
				t.Fatalf("unexpected LLM calls: %d, want %d", got, tc.calls)
			}
		})
	}
}
