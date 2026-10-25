package home

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestMCPReadsSSEStructuredResultAndRejectsToolErrors(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if body.Method == "initialize" {
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"protocolVersion\":\"2025-03-26\"}}\n\n", body.ID)
			return
		}
		if body.Params.Name == "ha_get_state" && body.Params.Arguments["entity_id"] == "light.office" {
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"structuredContent\":{\"data\":{\"entity_id\":\"light.office\",\"state\":\"on\"}},\"isError\":false}}\n\n", body.ID)
		} else {
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"isError\":true,\"content\":[{\"type\":\"text\",\"text\":\"service failed\"}]}}\n\n", body.ID)
		}
	}))
	defer s.Close()
	c := MCP{URL: s.URL}
	got, err := c.Call(t.Context(), "ha_get_state", map[string]any{"entity_id": "light.office"})
	if err != nil || string(got) != `{"data":{"entity_id":"light.office","state":"on"}}` {
		t.Fatalf("result=%s err=%v", got, err)
	}
	if _, err := c.Call(t.Context(), "unknown_tool", nil); err == nil {
		t.Fatal("HTTP 200 tool error treated as success")
	}
}

func TestMCPAllowsConcurrentIndependentReads(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.UnmarshalRead(r.Body, &b)
		if b.Method != "initialize" {
			arrived <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"structuredContent":{"success":true}}}`, b.ID)
	}))
	defer s.Close()
	c := MCP{URL: s.URL}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _, _ = c.Call(ctx, "ha_get_state", nil) })
	}
	for range 2 {
		select {
		case <-arrived:
		case <-ctx.Done():
			close(release)
			wg.Wait()
			t.Fatal("independent reads serialized")
		}
	}
	close(release)
	wg.Wait()
}
