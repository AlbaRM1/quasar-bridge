package assistant

import "testing"

func TestMCPConfigurationValidation(t *testing.T) {
	t.Setenv("MCP_URL_FIXTURE", "https://mcp.example.invalid/mcp/private-capability")
	t.Setenv("MCP_TOKEN_FIXTURE", "fixture-token")
	for _, valid := range []MCPServerConfig{
		{Name: "http", URL: "https://mcp.example.invalid/mcp"},
		{Name: "sse", Transport: "sse", URLEnv: "MCP_URL_FIXTURE", HeadersEnv: map[string]string{"X-API-Key": "MCP_TOKEN_FIXTURE"}},
		{Name: "local", Command: "fixture", Args: []string{"--stdio"}, EnvFrom: map[string]string{"TOKEN": "MCP_TOKEN_FIXTURE"}},
		{Name: "disabled", Disabled: true},
	} {
		if err := validateMCP([]MCPServerConfig{valid}); err != nil {
			t.Fatal(valid.Name, err)
		}
	}
	for _, invalid := range []MCPServerConfig{
		{Name: "UPPER", URL: "https://mcp.example.invalid/mcp"},
		{Name: "x", Transport: "unknown", URL: "https://mcp.example.invalid/mcp"},
		{Name: "x", URL: "https://user:password@mcp.example.invalid/mcp"},
		{Name: "x", URLEnv: "UNSET_MCP_URL_FIXTURE"},
		{Name: "x", URL: "https://mcp.example.invalid/mcp", URLEnv: "MCP_URL_FIXTURE"},
		{Name: "x", Transport: "stdio"},
		{Name: "x", Command: "fixture", URL: "https://mcp.example.invalid/mcp"},
		{Name: "x", URL: "https://mcp.example.invalid/mcp", TimeoutMS: -1},
		{Name: "x", URL: "https://mcp.example.invalid/mcp", BearerTokenEnv: "UNSET_MCP_TOKEN_FIXTURE"},
		{Name: "x", URL: "https://mcp.example.invalid/mcp", HeadersEnv: map[string]string{"MCP-Session-ID": "MCP_TOKEN_FIXTURE"}},
		{Name: "x", Command: "fixture", EnvFrom: map[string]string{"TOKEN": "UNSET_MCP_TOKEN_FIXTURE"}},
	} {
		if err := validateMCP([]MCPServerConfig{invalid}); err == nil {
			t.Fatal("invalid MCP configuration accepted", invalid.Name)
		}
	}
	cfg := DefaultConfig()
	cfg.MCPServers = []MCPServerConfig{{Name: "one", URL: "https://mcp.example.invalid/mcp"}}
	if cfg.Validate() == nil {
		t.Fatal("MCP silently enabled while assistant tools are disabled")
	}
	cfg.Enabled = true
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.MCPServers = append(cfg.MCPServers, cfg.MCPServers[0])
	if cfg.Validate() == nil {
		t.Fatal("duplicate server namespace accepted")
	}
}
