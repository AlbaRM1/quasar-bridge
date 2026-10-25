package assistant

import (
	"context"
	"encoding/json/v2"
	"errors"
	"regexp"
	"strings"
	"time"

	"quasarbridge/internal/journal"
	"quasarbridge/internal/llm"
)

type Tools struct {
	Config        Config
	JournalConfig func() journal.Config
	WebReader     *WebReader
	Now           func() time.Time
}

func definition(name, description string, properties map[string]any, required []string) llm.Tool {
	return llm.Tool{Type: "function", Function: llm.Function{Name: name, Description: description, Parameters: map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}}
}
func (t *Tools) Definitions() []llm.Tool {
	var tools []llm.Tool
	if t.Config.Web {
		tools = append(tools, definition("read_webpage", "Прочитать публичную HTTP/HTTPS страницу по URL. Возвращает текст без JavaScript. Не является поиском в интернете.", map[string]any{"url": map[string]string{"type": "string"}}, []string{"url"}))
	}
	if t.Config.History {
		tools = append(tools, definition("search_history", "Поиск сохранённых запросов колонки и доставленных ответов. query — ключевые слова (все должны встретиться), пустая строка — последние запросы. since/until — необязательные RFC3339 (until не включительно), limit от 1 до 20. Результаты от новых к старым.", map[string]any{"query": map[string]string{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}, "since": map[string]string{"type": "string"}, "until": map[string]string{"type": "string"}}, []string{"query"}))
	}
	if t.Config.Time {
		tools = append(tools, definition("current_time", "Текущие дата, время и часовой пояс пользователя.", map[string]any{}, []string{}))
	}
	return tools
}
func (t *Tools) Call(ctx context.Context, name, args string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) > 16000 {
		return nil, errors.New("tool arguments exceed limit")
	}
	switch {
	case name == "read_webpage" && t.Config.Web:
		var p struct {
			URL string `json:"url"`
		}
		if json.Unmarshal([]byte(args), &p, json.RejectUnknownMembers(true)) != nil || p.URL == "" {
			return nil, errors.New("read_webpage requires a URL")
		}
		return t.WebReader.Read(ctx, p.URL)
	case name == "search_history" && t.Config.History:
		var p HistoryQuery
		if json.Unmarshal([]byte(args), &p, json.RejectUnknownMembers(true)) != nil {
			return nil, errors.New("invalid search_history arguments")
		}
		result, err := SearchHistory(ctx, t.JournalConfig(), p, t.Config.HistoryMaxScanMB)
		if err != nil {
			return nil, err
		}
		loc, err := time.LoadLocation(t.Config.Timezone)
		if err != nil {
			return nil, errors.New("configured timezone unavailable")
		}
		for i := range result.Items {
			result.Items[i].Time = result.Items[i].Time.In(loc)
		}
		result.Timezone = t.Config.Timezone
		return result, nil
	case name == "current_time" && t.Config.Time:
		var p struct{}
		if json.Unmarshal([]byte(args), &p, json.RejectUnknownMembers(true)) != nil {
			return nil, errors.New("current_time takes no arguments")
		}
		loc, err := time.LoadLocation(t.Config.Timezone)
		if err != nil {
			return nil, errors.New("configured timezone unavailable")
		}
		now := time.Now()
		if t.Now != nil {
			now = t.Now()
		}
		now = now.In(loc)
		return map[string]string{"time": now.Format(time.RFC3339), "weekday": now.Weekday().String(), "timezone": t.Config.Timezone}, nil
	default:
		return nil, errors.New("unknown or disabled tool")
	}
}

// InitialCalls supplies cheap local context without an extra model round trip.
var explicitURL = regexp.MustCompile(`(?i)https?://[^\s<>"'«»]+`)

func (t *Tools) InitialCalls(text string) []llm.FunctionCall {
	original := text
	var calls []llm.FunctionCall
	if t.Config.Time {
		calls = append(calls, llm.FunctionCall{Name: "current_time", Arguments: "{}"})
	}
	text = strings.ReplaceAll(strings.ToLower(text), "ё", "е")
	history := false
	for _, marker := range []string{"в истории", "истории сообщени", "истории диалога", "истории запросов", "историю запросов", "что я спрашивал", "когда я спрашивал", "о чем мы с тобой", "что ты отвечал", "что ты ответил", "что я просил", "когда я просил"} {
		if strings.Contains(text, marker) {
			history = true
		}
	}
	// A request that explicitly combines a page with history must read the page first.
	web := strings.Contains(text, "https://") || strings.Contains(text, "http://") || strings.Contains(text, "страниц") || strings.Contains(text, "сайт")
	if t.Config.Web {
		if raw := explicitURL.FindString(original); raw != "" {
			args, _ := json.Marshal(map[string]string{"url": strings.TrimRight(raw, ".,;")})
			calls = append(calls, llm.FunctionCall{Name: "read_webpage", Arguments: string(args)})
			return calls
		}
	}
	if t.Config.History && history && !web {
		query := HistoryQuery{Limit: 8}
		if strings.Contains(text, "сегодня") || strings.Contains(text, "вчера") {
			loc, err := time.LoadLocation(t.Config.Timezone)
			if err == nil {
				now := time.Now()
				if t.Now != nil {
					now = t.Now()
				}
				now = now.In(loc)
				start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
				if strings.Contains(text, "вчера") {
					query.Until = start.Format(time.RFC3339)
					start = start.AddDate(0, 0, -1)
				}
				query.Since = start.Format(time.RFC3339)
			}
		}
		args, _ := json.Marshal(query)
		calls = append(calls, llm.FunctionCall{Name: "search_history", Arguments: string(args)})
	}
	return calls
}
