// Package assistant runs read-only tools for conversational LLM answers.
package assistant

import (
	"errors"
	"time"
	_ "time/tzdata" // Speaker firmware may not have an IANA zoneinfo database.
)

type Config struct {
	Enabled          bool   `json:"enabled"`
	Web              bool   `json:"web"`
	History          bool   `json:"history"`
	Time             bool   `json:"time"`
	Timezone         string `json:"timezone"`
	MaxRounds        int    `json:"max_rounds"`
	MaxCalls         int    `json:"max_calls"`
	WebTimeoutMS     int    `json:"web_timeout_ms"`
	WebMaxBytes      int    `json:"web_max_bytes"`
	WebMaxChars      int    `json:"web_max_chars"`
	HistoryMaxScanMB int    `json:"history_max_scan_mb"`
}

func DefaultConfig() Config {
	return Config{Enabled: false, Web: true, History: true, Time: true, Timezone: "UTC", MaxRounds: 3, MaxCalls: 4, WebTimeoutMS: 8000, WebMaxBytes: 1 << 20, WebMaxChars: 12000, HistoryMaxScanMB: 16}
}
func (c Config) Validate() error {
	if c.MaxRounds < 1 || c.MaxRounds > 5 || c.MaxCalls < 1 || c.MaxCalls > 10 || c.WebTimeoutMS < 100 || c.WebTimeoutMS > 20000 || c.WebMaxBytes < 1024 || c.WebMaxBytes > 2<<20 || c.WebMaxChars < 100 || c.WebMaxChars > 20000 || c.HistoryMaxScanMB < 1 || c.HistoryMaxScanMB > 64 {
		return errors.New("invalid assistant_tools limits")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return errors.New("invalid assistant_tools timezone")
	}
	return nil
}
