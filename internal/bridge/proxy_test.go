package bridge

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A proxy that decodes and reserializes unknown frames would corrupt future
// protocol fields or opaque audio. Exercise the real WebSocket path instead.
func TestProxyPreservesUnknownPayloadAndAuthentication(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" || r.URL.RawQuery != "feature=protobuf" {
			http.Error(w, "lost request context", http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			kind, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err := c.Write(r.Context(), kind, data); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	p := &Proxy{Upstream: "ws" + strings.TrimPrefix(upstream.URL, "http") + "/uni.ws"}
	server := httptest.NewServer(p)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/uni.ws?feature=protobuf", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer fixture-token"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	for _, data := range [][]byte{{0, 255, 1, 2, 128, 254}, []byte(`{"header":{"future":true},"payload":{"token":"not-for-logs"}}`)} {
		if err := c.Write(ctx, websocket.MessageBinary, data); err != nil {
			t.Fatal(err)
		}
		kind, got, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if kind != websocket.MessageBinary || !bytes.Equal(got, data) {
			t.Fatalf("payload changed: kind=%v bytes=%x", kind, got)
		}
	}
}

func TestProxyRejectsUnavailableUpstreamBeforeUpgrade(t *testing.T) {
	p := &Proxy{Upstream: "ws://127.0.0.1:1/uni.ws"}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/uni.ws", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502", w.Code)
	}
}
