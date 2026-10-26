package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"quasarbridge/internal/llm"
)

func TestCustomMCPProcess(t *testing.T) {
	if action := os.Getenv("QUASAR_MCP_CHECK_CHILD"); action != "" {
		flag.CommandLine = flag.NewFlagSet("quasar-bridge", flag.ExitOnError)
		os.Args = []string{"quasar-bridge", "-config", os.Getenv("QUASAR_MCP_CHECK_CONFIG"), "-" + action}
		if action == "query" {
			os.Args = append(os.Args, "спроси нейросеть: прочитай данные")
		}
		main()
		return
	}
	var toolCalls, llmCalls atomic.Int64
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		toolCalls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "through MCP"}}}, nil
	})
	mcpAPI := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	defer mcpAPI.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llm.Message `json:"messages"`
			Tools    []llm.Tool    `json:"tools"`
		}
		if err := json.UnmarshalRead(r.Body, &request); err != nil {
			t.Error(err)
			return
		}
		var message llm.Message
		if llmCalls.Add(1) == 1 {
			if len(request.Tools) != 1 || !strings.HasPrefix(request.Tools[0].Function.Name, "mcp__fixture__") {
				t.Error("MCP definitions did not reach LLM")
				return
			}
			message.ToolCalls = []llm.ToolCall{{ID: "mcp-1", Type: "function", Function: llm.FunctionCall{Name: request.Tools[0].Function.Name, Arguments: `{}`}}}
		} else {
			if !strings.Contains(request.Messages[len(request.Messages)-1].Content, "through MCP") {
				t.Error("MCP output did not return to LLM")
			}
			message.Content = "Ответ получен"
		}
		body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": "stop"}}})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer api.Close()
	body, _ := json.Marshal(map[string]any{"mode": "hybrid", "llm_base_url": api.URL + "/v1", "llm_model": "fixture", "assistant_tools": map[string]any{"enabled": true, "web": false, "history": false, "time": false, "mcp_servers": []any{map[string]any{"name": "fixture", "url": mcpAPI.URL}}}})
	path := configFile(t, string(body))
	for _, action := range []string{"check-mcp", "query"} {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCustomMCPProcess$")
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "QUASAR_") {
				child.Env = append(child.Env, entry)
			}
		}
		child.Env = append(child.Env, "QUASAR_HOME_MCP_URL=", "QUASAR_MCP_CHECK_CHILD="+action, "QUASAR_MCP_CHECK_CONFIG="+path)
		var stdout, stderr bytes.Buffer
		child.Stdout, child.Stderr = &stdout, &stderr
		err := child.Run()
		cancel()
		if err != nil {
			t.Fatalf("%s failed: %v %s", action, err, stderr.String())
		}
		if action == "check-mcp" {
			if !strings.Contains(stdout.String(), `"tool_names":["echo"]`) || toolCalls.Load() != 0 || llmCalls.Load() != 0 {
				t.Fatal("MCP discovery diagnostic executed an action", stdout.String())
			}
		} else if !strings.Contains(stdout.String(), "Ответ получен") || toolCalls.Load() != 1 || llmCalls.Load() != 2 {
			t.Fatal("CLI → LLM → MCP → LLM failed", stdout.String())
		}
	}
}
