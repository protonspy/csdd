package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiffSpecs(t *testing.T) {
	old := map[string]specState{
		"kept":    {Phase: "design", Approved: []string{"requirements"}},
		"changed": {Phase: "requirements"},
		"gone":    {Phase: "tasks"},
	}
	cur := map[string]specState{
		"kept":    {Phase: "design", Approved: []string{"requirements"}}, // unchanged → no msg
		"changed": {Phase: "requirements-approved", Approved: []string{"requirements"}},
		"new":     {Phase: "requirements"},
	}
	msgs := diffSpecs(old, cur)
	joined := strings.Join(msgs, "\n")

	if strings.Contains(joined, "kept") {
		t.Errorf("unchanged spec should not appear:\n%s", joined)
	}
	assertContains(t, joined, "🆕 spec new")
	assertContains(t, joined, "📋 spec changed")
	assertContains(t, joined, "requirements-approved")
	assertContains(t, joined, "🗑 spec gone")
}

func TestDiffSpecsDetectsApprovalOnlyChange(t *testing.T) {
	old := map[string]specState{"f": {Phase: "tasks-approved", Approved: []string{"requirements", "design"}}}
	cur := map[string]specState{"f": {Phase: "tasks-approved", Approved: []string{"requirements", "design", "tasks"}, Ready: true}}
	msgs := diffSpecs(old, cur)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 change message, got %d: %v", len(msgs), msgs)
	}
	assertContains(t, msgs[0], "tasks")
	assertContains(t, msgs[0], "pronta")
}

func TestReadSpecSnapshot(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "albums", `{
		"phase":"design",
		"ready_for_implementation":false,
		"approvals":{"requirements":{"approved":true},"design":{"approved":false}}
	}`)
	writeSpec(t, root, "auth", `{
		"phase":"tasks-approved",
		"ready_for_implementation":true,
		"approvals":{"requirements":{"approved":true},"design":{"approved":true},"tasks":{"approved":true}}
	}`)

	snap := readSpecSnapshot(root)
	if len(snap) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(snap))
	}
	if got := snap["albums"]; got.Phase != "design" || len(got.Approved) != 1 || got.Approved[0] != "requirements" || got.Ready {
		t.Fatalf("albums snapshot wrong: %+v", got)
	}
	if got := snap["auth"]; !got.Ready || len(got.Approved) != 3 {
		t.Fatalf("auth snapshot wrong: %+v", got)
	}
}

func TestReadSpecSnapshotMissingDir(t *testing.T) {
	if snap := readSpecSnapshot(t.TempDir()); len(snap) != 0 {
		t.Fatalf("expected empty snapshot with no specs dir, got %v", snap)
	}
}

func TestPlanUpdatesSeedsThenTails(t *testing.T) {
	root := t.TempDir()
	log := writePlanLog(t, root, "ship-it", "## [2026-07-08] spec:requirements | feat-a | ok\n")

	n := &Notifier{root: root, offsets: map[string]int64{}}

	// First poll seeds existing history to EOF and relays nothing.
	if got := n.planUpdates(); len(got) != 0 {
		t.Fatalf("first poll should seed, not relay; got %v", got)
	}

	// Appended lines are relayed on the next poll, prefixed by slug.
	appendFile(t, log, "## [2026-07-08] impl:feat-a | feat-a | ok\n")
	got := n.planUpdates()
	if _, ok := got["ship-it"]; !ok {
		t.Fatalf("expected an update for ship-it, got %v", got)
	}
	assertContains(t, got["ship-it"], "impl:feat-a")
	if strings.Contains(got["ship-it"], "spec:requirements") {
		t.Fatalf("seeded history should not be relayed; got %q", got["ship-it"])
	}

	// No further appends → nothing to relay.
	if got := n.planUpdates(); len(got) != 0 {
		t.Fatalf("expected no updates when nothing changed, got %v", got)
	}
}

func TestPlanUpdatesRelaysPlansAppearingAfterStartup(t *testing.T) {
	root := t.TempDir()
	// Force the seeded state without any plans present.
	n := &Notifier{root: root, offsets: map[string]int64{}}
	n.planUpdates()

	writePlanLog(t, root, "late", "## [2026-07-08] spec:requirements | late | ok\n")
	got := n.planUpdates()
	if _, ok := got["late"]; !ok {
		t.Fatalf("a plan created after startup should relay from the top; got %v", got)
	}
	assertContains(t, got["late"], "spec:requirements")
}

// TestNotifierFlushesFinalLinesOnCancel proves the embedded plan-run integration
// delivers a run's last journal lines: with a ticker that never fires, the only
// way the appended line reaches the chat is the flush the notifier runs when its
// context is cancelled (which is exactly what `plan run` does when the loop ends).
func TestNotifierFlushesFinalLinesOnCancel(t *testing.T) {
	root := t.TempDir()
	log := writePlanLog(t, root, "ship-it", "## [old] seed | x | ok\n")

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

	n := NewNotifier(Options{
		Root:     root,
		Client:   NewClient("t", "1", srv.URL),
		Interval: time.Hour, // the ticker must never fire — only startup + flush send
		Out:      io.Discard,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = n.Run(ctx); close(done) }()

	// Wait until Run has seeded the log to EOF and sent its startup banner.
	waitFor(t, func() bool { return has("csdd telegram ligado") })

	// A run line appended after seeding; the hourly ticker will not fire, so only
	// the cancel-flush can deliver it.
	appendFile(t, log, "## [2026-07-08] - | feat-a | done\n")
	cancel()
	<-done

	if !has("feat-a") {
		t.Fatalf("cancel-flush should relay the final journal line; got %v", got)
	}
}

// waitFor polls cond until it holds or a short deadline passes.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within the timeout")
}

// --- helpers ---------------------------------------------------------------

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected to find %q in:\n%s", needle, haystack)
	}
}

func writeSpec(t *testing.T, root, name, specJSON string) {
	t.Helper()
	dir := filepath.Join(root, "specs", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.json"), []byte(specJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePlanLog(t *testing.T, root, slug, content string) string {
	t.Helper()
	dir := filepath.Join(root, "docs", "plans", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "log.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}
