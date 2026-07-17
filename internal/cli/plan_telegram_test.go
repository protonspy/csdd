package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/protonspy/csdd/internal/telegram"
)

// TestStartPlanTelegramNoBotIsNoop: with no .csdd/bot.json, the auto-start is a
// silent no-op whose stop function is safe to call — a plan run without a bot is
// completely unaffected.
func TestStartPlanTelegramNoBotIsNoop(t *testing.T) {
	stop := startPlanTelegram(t.TempDir())
	stop() // must not panic or block
}

// TestStartPlanTelegramRelaysWhenConfigured: a configured bot makes the plan-run
// auto-start relay the run journal to the chat, and stop() flushes cleanly.
func TestStartPlanTelegramRelaysWhenConfigured(t *testing.T) {
	root := t.TempDir()
	if err := telegram.Save(root, telegram.Config{Token: "t", ChatID: "1"}); err != nil {
		t.Fatal(err)
	}
	// A pre-existing run journal, so the notifier has a plan to watch.
	logPath := filepath.Join(root, "docs", "plans", "ship-it", "log.md")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("## [old] seed | x | ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		got = append(got, body.Text)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	t.Setenv("TELEGRAM_API_BASE", srv.URL)
	has := func(sub string) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, m := range got {
			if strings.Contains(m, sub) {
				return true
			}
		}
		return false
	}

	stop := startPlanTelegram(root)
	// Wait for the startup banner, then append a run line and stop; the flush must
	// relay it.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !has("csdd telegram ligado") {
		time.Sleep(10 * time.Millisecond)
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("## [2026-07-08] - | feat-a | done\n")
	_ = f.Close()
	stop()

	if !has("csdd telegram ligado") {
		t.Errorf("expected the startup banner to be relayed; got %v", got)
	}
	if !has("feat-a") {
		t.Errorf("expected the appended journal line to be flushed on stop; got %v", got)
	}
}
