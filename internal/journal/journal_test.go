package journal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotationPrivacyAndFailedReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "turns.jsonl")
	w := new(Writer)
	defer w.Close()
	c := DefaultConfig()
	c.Enabled = true
	c.Path = path
	c.MaxSizeMB = 1
	c.MaxBackups = 2
	c.MaxTextChars = 1000
	if err := w.Configure(c); err != nil {
		t.Fatal(err)
	}
	for range 1100 {
		w.Record(Event{Event: "request", Text: strings.Repeat("я", 1100)})
	}
	for _, name := range []string{path, path + ".1", path + ".2"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 || info.Size() > 1<<20 {
			t.Fatalf("bad file: %s %v %d", name, info.Mode(), info.Size())
		}
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"truncated":true`) {
		t.Fatal("missing truncation")
	}
	bad := c
	bad.Path = filepath.Dir(path)
	if w.Configure(bad) == nil {
		t.Fatal("accepted directory")
	}
	w.Record(Event{Event: "response", Text: "still enabled"})
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "still enabled") {
		t.Fatal("lost old writer")
	}
	c.Enabled = false
	if err := w.Configure(c); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	w.Record(Event{Text: "disabled"})
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("disabled writer wrote data")
	}
}

func TestRotationFailureRecoversAfterDestinationIsFixed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turns.jsonl")
	w := new(Writer)
	defer w.Close()
	c := DefaultConfig()
	c.Enabled = true
	c.Path = path
	c.MaxSizeMB = 1
	c.MaxBackups = 1
	c.MaxTextChars = 64000
	if err := w.Configure(c); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".1", 0700); err != nil {
		t.Fatal(err)
	}
	for range 30 {
		w.Record(Event{Event: "request", Text: strings.Repeat("x", 64000)})
	}
	if w.Failures() == 0 {
		t.Fatal("missing write failure")
	}
	if err := os.Remove(path + ".1"); err != nil {
		t.Fatal(err)
	}
	w.Record(Event{Event: "response", Text: "recovered"})
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "recovered") {
		t.Fatal("writer stayed broken after rotation recovered")
	}
}

func TestReducingRetentionRemovesOldBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turns.jsonl")
	w := new(Writer)
	defer w.Close()
	c := DefaultConfig()
	c.Enabled = true
	c.Path = path
	c.MaxBackups = 4
	if err := w.Configure(c); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		if err := os.WriteFile(fmt.Sprintf("%s.%d", path, i), []byte("private old text"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c.MaxBackups = 1
	if err := w.Configure(c); err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 4; i++ {
		if _, err := os.Stat(fmt.Sprintf("%s.%d", path, i)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("old archive retained")
		}
	}
}
