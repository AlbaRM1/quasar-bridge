package main

import (
	"encoding/json/v2"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"

	"quasarbridge/internal/assistant"
	"quasarbridge/internal/bridge"
	"quasarbridge/internal/dialogue"
	"quasarbridge/internal/journal"
)

type progressConfig struct {
	Enabled    bool `json:"enabled"`
	AfterMS    int  `json:"after_ms"`
	IntervalMS int  `json:"interval_ms"`
}

type config struct {
	DialogueContext  dialogue.Config      `json:"dialogue_context"`
	LocalProgress    progressConfig       `json:"local_progress"`
	AssistantTools   assistant.Config     `json:"assistant_tools"`
	ConversationLog  journal.Config       `json:"conversation_log"`
	Listen           string               `json:"listen"`
	Upstream         string               `json:"upstream"`
	TLSCert          string               `json:"tls_cert"`
	TLSKey           string               `json:"tls_key"`
	AllowedIPs       []string             `json:"allowed_ips"`
	Mode             string               `json:"mode"`
	LLMBaseURL       string               `json:"llm_base_url"`
	LLMModel         string               `json:"llm_model"`
	HomeWarmArea     string               `json:"home_warm_area"`
	Routing          bridge.RoutingConfig `json:"routing"`
	SystemPrompt     string               `json:"system_prompt"`
	ListenAfterReply *bool                `json:"listen_after_reply"`
	LLMExtraBody     map[string]any       `json:"llm_extra_body"`
}

func readConfig(path string) (config, error) {
	cfg := config{DialogueContext: dialogue.DefaultConfig(), Routing: bridge.DefaultRoutingConfig(), ConversationLog: journal.DefaultConfig(), AssistantTools: assistant.DefaultConfig(), LocalProgress: progressConfig{AfterMS: 2000, IntervalMS: 4000}}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, errors.New("cannot read configuration file")
	}
	if json.Unmarshal(raw, &cfg, json.RejectUnknownMembers(true)) != nil {
		return cfg, errors.New("invalid configuration JSON")
	}
	for name, target := range map[string]*string{
		"QUASAR_LISTEN": &cfg.Listen, "QUASAR_UPSTREAM": &cfg.Upstream,
		"QUASAR_TLS_CERT": &cfg.TLSCert, "QUASAR_TLS_KEY": &cfg.TLSKey,
		"QUASAR_MODE": &cfg.Mode, "QUASAR_LLM_BASE_URL": &cfg.LLMBaseURL,
		"QUASAR_LLM_MODEL": &cfg.LLMModel, "QUASAR_HOME_WARM_AREA": &cfg.HomeWarmArea,
		"QUASAR_SYSTEM_PROMPT": &cfg.SystemPrompt,
	} {
		if value, ok := os.LookupEnv(name); ok {
			*target = value
		}
	}
	if value, ok := os.LookupEnv("QUASAR_ALLOWED_IPS"); ok {
		cfg.AllowedIPs = nil
		for item := range strings.SplitSeq(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				cfg.AllowedIPs = append(cfg.AllowedIPs, item)
			}
		}
	}
	if len(cfg.AllowedIPs) == 0 {
		cfg.AllowedIPs = nil
	}
	if len(cfg.LLMExtraBody) == 0 {
		cfg.LLMExtraBody = nil
	}
	if cfg.Listen == "" {
		cfg.Listen = "localhost:8443"
	}
	if cfg.Upstream == "" {
		cfg.Upstream = "wss://uniproxy.alice.yandex.net/uni.ws"
	}
	if cfg.Mode == "" {
		cfg.Mode = "observe"
	}
	if _, _, err := net.SplitHostPort(cfg.Listen); err != nil {
		return cfg, errors.New("listen must include host and port")
	}
	if cfg.Mode != "observe" && cfg.Mode != "hybrid" {
		return cfg, errors.New("mode must be observe or hybrid")
	}
	if !validEndpoint(cfg.Upstream, "ws", "wss") {
		return cfg, errors.New("invalid upstream URL")
	}
	if (cfg.TLSCert == "") != (cfg.TLSKey == "") {
		return cfg, errors.New("TLS certificate and key must be set together")
	}
	for _, ip := range cfg.AllowedIPs {
		if _, err := netip.ParseAddr(ip); err != nil {
			return cfg, errors.New("allowed_ips must contain IP addresses")
		}
	}
	if cfg.LLMBaseURL != "" && !validEndpoint(cfg.LLMBaseURL, "http", "https") {
		return cfg, errors.New("invalid LLM URL")
	}
	if cfg.Mode == "hybrid" {
		if cfg.LLMBaseURL == "" || cfg.LLMModel == "" {
			return cfg, errors.New("hybrid mode requires LLM URL and model")
		}
		if endpoint := os.Getenv("QUASAR_HOME_MCP_URL"); endpoint != "" && !validEndpoint(endpoint, "http", "https") {
			return cfg, errors.New("invalid QUASAR_HOME_MCP_URL")
		}
	}
	if cfg.LocalProgress.AfterMS < 100 || cfg.LocalProgress.AfterMS > 10000 || cfg.LocalProgress.IntervalMS < 1000 || cfg.LocalProgress.IntervalMS > 10000 {
		return cfg, errors.New("invalid local_progress timing")
	}
	if err := cfg.DialogueContext.Validate(); err != nil {
		return cfg, err
	}
	if err := cfg.AssistantTools.Validate(); err != nil {
		return cfg, err
	}
	if err := cfg.ConversationLog.Validate(); err != nil {
		return cfg, err
	}
	if _, err := bridge.CompileRouting(cfg.Routing); err != nil {
		return cfg, err
	}
	return cfg, nil
}
func validEndpoint(raw string, schemes ...string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	for _, scheme := range schemes {
		if u.Scheme == scheme {
			return true
		}
	}
	return false
}
