package home

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// URL may contain a capability token: never include it in an error or log.
type MCP struct {
	URL         string
	HTTP        *http.Client
	mu          sync.Mutex
	initialized bool
	sequence    atomic.Int64
	sessionMu   sync.RWMutex
	session     string
}
type rpcResponse struct {
	ID     int64          `json:"id"`
	Result jsontext.Value `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func (c *MCP) Call(ctx context.Context, name string, args map[string]any) (jsontext.Value, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c.mu.Lock()
	if !c.initialized {
		if _, err := c.request(ctx, "initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "quasar-bridge", "version": "0.1"}}); err != nil {
			c.mu.Unlock()
			return nil, err
		}
		c.initialized = true
	}
	c.mu.Unlock()
	raw, err := c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	var result struct {
		IsError    bool           `json:"isError"`
		Structured jsontext.Value `json:"structuredContent"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, errors.New("invalid Home Assistant tool response")
	}
	if result.IsError {
		return nil, fmt.Errorf("Home Assistant tool %s failed", name)
	}
	data := result.Structured
	if len(data) == 0 || string(data) == "null" {
		for _, item := range result.Content {
			if item.Type == "text" && jsontext.Value(item.Text).IsValid() {
				data = jsontext.Value(item.Text)
				break
			}
		}
	}
	if len(data) == 0 {
		return nil, errors.New("Home Assistant returned no structured result")
	}
	var outcome struct {
		Success *bool `json:"success"`
	}
	if json.Unmarshal(data, &outcome) == nil && outcome.Success != nil && !*outcome.Success {
		return nil, fmt.Errorf("Home Assistant tool %s reported failure", name)
	}
	return data, nil
}

func (c *MCP) request(ctx context.Context, method string, params any) (jsontext.Value, error) {
	id := c.sequence.Add(1)
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid Home Assistant endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-03-26")
	c.sessionMu.RLock()
	session := c.session
	c.sessionMu.RUnlock()
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Home Assistant transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Home Assistant HTTP %d", resp.StatusCode)
	}
	if session := resp.Header.Get("Mcp-Session-Id"); session != "" {
		c.sessionMu.Lock()
		c.session = session
		c.sessionMu.Unlock()
	}
	var result rpcResponse
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, 8<<20))
		scanner.Buffer(make([]byte, 4096), 8<<20)
		found := false
		for scanner.Scan() {
			line := scanner.Text()
			if payload, ok := strings.CutPrefix(line, "data:"); ok && json.Unmarshal([]byte(strings.TrimSpace(payload)), &result) == nil && result.ID == id {
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("Home Assistant returned no matching SSE response")
		}
	} else {
		if err := json.UnmarshalRead(io.LimitReader(resp.Body, 8<<20), &result); err != nil {
			return nil, errors.New("invalid Home Assistant JSON response")
		}
	}
	if result.ID != id {
		return nil, errors.New("Home Assistant response ID mismatch")
	}
	if result.Error != nil {
		return nil, fmt.Errorf("Home Assistant RPC error %d", result.Error.Code)
	}
	return result.Result, nil
}
