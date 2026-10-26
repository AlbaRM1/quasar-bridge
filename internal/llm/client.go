package llm

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Reasoning  string     `json:"reasoning_content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}
type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}
type Client struct {
	BaseURL   string
	Model     string
	APIKey    string
	HTTP      *http.Client
	ExtraBody map[string]any
}

func (c *Client) Complete(ctx context.Context, messages []Message, structured bool) (string, error) {
	m, err := c.chat(ctx, messages, structured, nil)
	if err != nil {
		return "", err
	}
	if len(m.ToolCalls) > 0 {
		return "", errors.New("unexpected tool call in plain completion")
	}
	return strings.TrimSpace(m.Content), nil
}
func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	return c.chat(ctx, messages, false, tools)
}
func (c *Client) chat(ctx context.Context, messages []Message, structured bool, tools []Tool) (Message, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	request := map[string]any{"model": c.Model, "messages": messages, "stream": false, "max_tokens": 1024, "temperature": 0, "chat_template_kwargs": map[string]any{"enable_thinking": false}}
	if len(tools) > 0 {
		request["tools"] = tools
		request["tool_choice"] = "auto"
		request["parallel_tool_calls"] = false
	}
	if structured {
		request["response_format"] = map[string]string{"type": "json_object"}
	}
	// Провайдеры вроде DeepSeek в thinking-режиме требуют собственные поля;
	// настройки не должны перетирать контракт клиента.
	for key, value := range c.ExtraBody {
		if _, exists := request[key]; !exists {
			request[key] = value
		}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return Message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, errors.New("invalid LLM endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Message{}, fmt.Errorf("LLM transport: %w", ctx.Err())
		}
		return Message{}, errors.New("LLM transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Message{}, fmt.Errorf("LLM returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Message{}, err
	}
	var result struct {
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Message{}, errors.New("invalid LLM response")
	}
	if len(result.Choices) == 0 || (strings.TrimSpace(result.Choices[0].Message.Content) == "" && len(result.Choices[0].Message.ToolCalls) == 0) {
		return Message{}, errors.New("LLM returned no answer")
	}
	if result.Choices[0].FinishReason == "length" {
		return Message{}, errors.New("LLM answer exceeded token budget")
	}
	m := result.Choices[0].Message
	m.Role = "assistant"
	return m, nil
}
