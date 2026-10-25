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
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Client struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client
}

func (c *Client) Complete(ctx context.Context, messages []Message, structured bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	request := map[string]any{"model": c.Model, "messages": messages, "stream": false, "max_tokens": 1024, "temperature": 0, "chat_template_kwargs": map[string]any{"enable_thinking": false}}
	if structured {
		request["response_format"] = map[string]string{"type": "json_object"}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", errors.New("invalid LLM endpoint")
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
			return "", fmt.Errorf("LLM transport: %w", ctx.Err())
		}
		return "", errors.New("LLM transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	var result struct {
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", errors.New("invalid LLM response")
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", errors.New("LLM returned no answer")
	}
	if result.Choices[0].FinishReason == "length" {
		return "", errors.New("LLM answer exceeded token budget")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}
