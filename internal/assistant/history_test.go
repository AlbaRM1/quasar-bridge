package assistant

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"quasarbridge/internal/journal"
	"strings"
	"testing"
	"time"
)

func TestHistorySearchJoinsRotatedVoiceRequestsAndDeliveredAnswers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turns.jsonl")
	cfg := journal.DefaultConfig()
	cfg.Path = path
	cfg.MaxBackups = 2
	stamp := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	write := func(path string, events []journal.Event) {
		t.Helper()
		var rows []string
		for _, e := range events {
			raw, _ := json.Marshal(e)
			rows = append(rows, string(raw))
		}
		if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path+".1", []journal.Event{{Time: stamp, Session: "s", Request: "r", Event: "request"}, {Time: stamp, Session: "s", Request: "r", Event: "transcript", Text: "Какая погода в Москве?"}})
	write(path, []journal.Event{{Time: stamp.Add(time.Second), Session: "s", Request: "r", Event: "cloud_response", Speech: "cloud duplicate"}, {Time: stamp.Add(time.Second), Session: "s", Request: "r", Event: "response", Speech: "Солнечно", Route: "yandex", Delivered: true}, {Time: stamp.Add(time.Minute), Session: "s", Request: "r2", Event: "request", Text: "Сколько будет два плюс два"}, {Time: stamp.Add(time.Minute), Session: "s", Request: "r2", Event: "response", Text: "Четыре", Delivered: true}})
	got, err := SearchHistory(t.Context(), cfg, HistoryQuery{Query: "москве солнечно", Limit: 5}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Answer != "Солнечно" || got.Items[0].Query != "Какая погода в Москве?" {
		t.Fatalf("bad history: %+v", got)
	}
	got, err = SearchHistory(t.Context(), cfg, HistoryQuery{Limit: 1}, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].Query != "Сколько будет два плюс два" {
		t.Fatalf("bad latest: %+v %v", got, err)
	}
	if _, err = SearchHistory(t.Context(), cfg, HistoryQuery{Since: "not-a-date"}, 1); err == nil {
		t.Fatal("invalid date accepted")
	}
}
