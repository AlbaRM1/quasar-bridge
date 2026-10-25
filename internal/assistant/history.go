package assistant

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"quasarbridge/internal/journal"
)

type HistoryQuery struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
	Since string `json:"since"`
	Until string `json:"until"`
}
type HistoryItem struct {
	Time   time.Time `json:"time"`
	Query  string    `json:"query"`
	Answer string    `json:"answer"`
	Route  string    `json:"route,omitempty"`
}
type HistoryResult struct {
	Timezone  string        `json:"timezone,omitempty"`
	Items     []HistoryItem `json:"items"`
	Partial   bool          `json:"partial,omitzero"`
	Recording bool          `json:"recording"`
}
type historyFile struct {
	file         *os.File
	offset, size int64
}

func SearchHistory(ctx context.Context, c journal.Config, q HistoryQuery, maxScanMB int) (HistoryResult, error) {
	result := HistoryResult{Items: []HistoryItem{}, Recording: c.Enabled}
	if q.Limit == 0 {
		q.Limit = 5
	}
	if q.Limit < 1 || q.Limit > 20 || len([]rune(q.Query)) > 200 {
		return result, errors.New("invalid history query or limit")
	}
	since := time.Time{}
	if q.Since != "" {
		var err error
		since, err = time.Parse(time.RFC3339, q.Since)
		if err != nil {
			return result, errors.New("since must be an RFC3339 timestamp")
		}
	}
	until := time.Time{}
	if q.Until != "" {
		var err error
		until, err = time.Parse(time.RFC3339, q.Until)
		if err != nil || (!since.IsZero() && !until.After(since)) {
			return result, errors.New("until must be an RFC3339 timestamp after since")
		}
	}
	if c.Path == "" || maxScanMB < 1 {
		return result, errors.New("history is not configured")
	}
	budget := int64(maxScanMB) << 20
	var files []historyFile
	defer func() {
		for _, f := range files {
			f.file.Close()
		}
	}()
	// Reserve bytes for newest files first, then read them in chronological order.
	for i := 0; i <= c.MaxBackups; i++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		path := c.Path
		if i > 0 {
			path = fmt.Sprintf("%s.%d", c.Path, i)
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return result, errors.New("cannot read history file")
		}
		if budget <= 0 {
			result.Partial = true
			break
		}
		f, err := os.Open(path)
		if err != nil {
			return result, errors.New("cannot open history file")
		}
		info, err = f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			f.Close()
			return result, errors.New("cannot read history file")
		}
		size := min(info.Size(), budget)
		offset := info.Size() - size
		if offset > 0 {
			result.Partial = true
		}
		files = append(files, historyFile{file: f, offset: offset, size: size})
		budget -= size
	}
	pending := map[string]HistoryItem{}
	hits := map[string]HistoryItem{}
	normalize := func(s string) string { return strings.ReplaceAll(strings.ToLower(s), "ё", "е") }
	keywords := strings.Fields(normalize(q.Query))
	trim := func(s string) string {
		r := []rune(s)
		if len(r) > 1200 {
			return string(r[:1200]) + "…"
		}
		return s
	}
	add := func(key string, item HistoryItem) {
		if item.Time.Before(since) || (!until.IsZero() && !item.Time.Before(until)) {
			return
		}
		text := normalize(item.Query + " " + item.Answer)
		for _, word := range keywords {
			if !strings.Contains(text, word) {
				return
			}
		}
		item.Query = trim(item.Query)
		item.Answer = trim(item.Answer)
		hits[key] = item
		if len(hits) > q.Limit {
			oldest := ""
			for k, v := range hits {
				if oldest == "" || v.Time.Before(hits[oldest].Time) {
					oldest = k
				}
			}
			delete(hits, oldest)
		}
	}
	for i := len(files) - 1; i >= 0; i-- {
		f := files[i]
		if _, err := f.file.Seek(f.offset, io.SeekStart); err != nil {
			return result, errors.New("cannot seek history file")
		}
		scanner := bufio.NewScanner(io.LimitReader(f.file, f.size))
		scanner.Buffer(make([]byte, 4096), 1<<20)
		skip := f.offset > 0
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if skip {
				skip = false
				continue
			}
			var e journal.Event
			if json.Unmarshal(scanner.Bytes(), &e) != nil {
				result.Partial = true
				continue
			}
			if e.Request == "" || e.Session == "" {
				continue
			}
			key := e.Session + ":" + e.Request
			p := pending[key]
			switch e.Event {
			case "request", "transcript":
				if p.Time.IsZero() {
					p.Time = e.Time
				}
				if e.Text != "" {
					p.Query = e.Text
				}
				pending[key] = p
			case "response":
				if !e.Delivered {
					continue
				}
				if p.Time.IsZero() {
					p.Time = e.Time
				}
				p.Answer = e.Speech
				if p.Answer == "" {
					p.Answer = e.Text
				}
				p.Route = e.Route
				if p.Query != "" || p.Answer != "" {
					add(key, p)
				}
				delete(pending, key)
			}
			if len(pending) > 256 {
				oldest := ""
				for k, v := range pending {
					if oldest == "" || v.Time.Before(pending[oldest].Time) {
						oldest = k
					}
				}
				p := pending[oldest]
				if p.Query != "" {
					add(oldest, p)
				}
				delete(pending, oldest)
			}
		}
		if scanner.Err() != nil {
			result.Partial = true
		}
	}
	// Do not include the question currently waiting for its own tool result.
	for key, p := range pending {
		if p.Query != "" && time.Since(p.Time) > 90*time.Second {
			add(key, p)
		}
	}
	for _, p := range hits {
		result.Items = append(result.Items, p)
	}
	slices.SortFunc(result.Items, func(a, b HistoryItem) int { return b.Time.Compare(a.Time) })
	return result, nil
}
