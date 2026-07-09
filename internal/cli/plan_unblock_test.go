package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protonspy/csdd/internal/plan"
)

// blockedPlan lays down the authored plan and blocks two feats: `upload` on a
// mechanical gate failure and `thumbs` on a deviation.
func blockedPlan(t *testing.T) string {
	t.Helper()
	dir := freshWorkspace(t)
	writeTestFile(t, filepath.Join(dir, "docs", "plans", "photos", "plan.md"), authoredPlan)
	if code, _, e := run(t, "plan", "approve", "photos", "--root", dir); code != 0 {
		t.Fatalf("plan approve failed: %s", e)
	}
	must(t, plan.WriteBlock(dir, "photos", plan.Block{
		Feat: "upload", Step: "task 1", Kind: plan.BlockGateFailure,
		Reason: "gates failed after 3 attempts: boom", Repairs: 2,
		Log: ".csdd/plan/photos/failures/upload/task-1.log",
	}))
	must(t, plan.WriteBlock(dir, "photos", plan.Block{
		Feat: "thumbs", Step: "spec-design", Kind: plan.BlockDeviation,
		Reason: "session blocked: needs a queue", Revision: "add a queue feat",
		PlanHash: "stale-hash",
	}))
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPlanUnblockNeedsATargetAndListsWhatIsBlocked(t *testing.T) {
	dir := blockedPlan(t)
	code, out, _ := run(t, "plan", "unblock", "photos", "--root", dir)
	if code != 1 {
		t.Errorf("naming no feat and no --all is a usage error, got %d", code)
	}
	for _, want := range []string{"upload", "gate-failure", "thumbs", "deviation", "failures/upload/task-1.log"} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing should show %q:\n%s", want, out)
		}
	}
}

func TestPlanUnblockClearsAMechanicalBlock(t *testing.T) {
	dir := blockedPlan(t)
	code, out, errOut := run(t, "plan", "unblock", "photos", "upload", "--root", dir)
	if code != 0 {
		t.Fatalf("unblocking a mechanical block should succeed (code=%d): %s %s", code, out, errOut)
	}
	if _, blocked := plan.ReadBlock(dir, "photos", "upload"); blocked {
		t.Errorf("upload should no longer be blocked")
	}
	if _, blocked := plan.ReadBlock(dir, "photos", "thumbs"); !blocked {
		t.Errorf("unblocking one feat must not touch the others")
	}
	logData, _ := os.ReadFile(filepath.Join(dir, "docs", "plans", "photos", "log.md"))
	if !strings.Contains(string(logData), "| upload | unblocked") {
		t.Errorf("the unblock should be journaled: %s", logData)
	}
}

func TestPlanUnblockRefusesANamedDeviationWithoutForce(t *testing.T) {
	dir := blockedPlan(t)
	code, out, errOut := run(t, "plan", "unblock", "photos", "thumbs", "--root", dir)
	if code != 2 {
		t.Errorf("a named deviation must be refused with exit 2, got %d", code)
	}
	if _, blocked := plan.ReadBlock(dir, "photos", "thumbs"); !blocked {
		t.Errorf("the deviation must survive a refusal")
	}
	all := out + errOut
	if !strings.Contains(all, "add a queue feat") || !strings.Contains(all, "plan approve photos") {
		t.Errorf("the refusal should surface the proposal and the real way out:\n%s", all)
	}

	// --force is the escape hatch, and it says so.
	if code, _, e := run(t, "plan", "unblock", "photos", "thumbs", "--force", "--root", dir); code != 0 {
		t.Fatalf("--force should clear a deviation (code=%d): %s", code, e)
	}
	if _, blocked := plan.ReadBlock(dir, "photos", "thumbs"); blocked {
		t.Errorf("--force should have cleared the deviation")
	}
}

func TestPlanUnblockAllSkipsDeviationsButClearsTheRest(t *testing.T) {
	dir := blockedPlan(t)
	code, out, errOut := run(t, "plan", "unblock", "photos", "--all", "--root", dir)
	if code != 0 {
		t.Errorf("--all clears what it can and warns about the rest, got %d", code)
	}
	if _, blocked := plan.ReadBlock(dir, "photos", "upload"); blocked {
		t.Errorf("--all should clear the mechanical block")
	}
	if _, blocked := plan.ReadBlock(dir, "photos", "thumbs"); !blocked {
		t.Errorf("--all must not silently drop a deviation")
	}
	if !strings.Contains(out+errOut, "left blocked") {
		t.Errorf("the skipped deviation should be reported:\n%s%s", out, errOut)
	}
}

func TestPlanUnblockRejectsAFeatThatIsNotBlocked(t *testing.T) {
	dir := blockedPlan(t)
	code, _, errOut := run(t, "plan", "unblock", "photos", "nope", "--root", dir)
	if code != 1 {
		t.Errorf("an unknown feat is an error, got %d", code)
	}
	if !strings.Contains(errOut, "not blocked") {
		t.Errorf("the error should say why: %s", errOut)
	}
}

func TestPlanUnblockOnACleanPlanIsANoop(t *testing.T) {
	dir := freshWorkspace(t)
	writeTestFile(t, filepath.Join(dir, "docs", "plans", "photos", "plan.md"), authoredPlan)
	code, out, _ := run(t, "plan", "unblock", "photos", "--all", "--root", dir)
	if code != 0 || !strings.Contains(out, "no blocked feats") {
		t.Errorf("unblocking a clean plan should succeed quietly, got %d: %s", code, out)
	}
}

func TestPlanApproveRetiresDeviationsRaisedAgainstTheOldPlan(t *testing.T) {
	dir := blockedPlan(t)
	// The deviation carries a stale hash, so re-approving the (revised) plan is
	// what answers it — no unblock command needed.
	code, out, errOut := run(t, "plan", "approve", "photos", "--root", dir)
	if code != 0 {
		t.Fatalf("re-approve failed (code=%d): %s", code, errOut)
	}
	if !strings.Contains(out, "unblocked thumbs") {
		t.Errorf("approving a revised plan should retire its deviation:\n%s", out)
	}
	if _, blocked := plan.ReadBlock(dir, "photos", "thumbs"); blocked {
		t.Errorf("the deviation should be gone")
	}
	// The mechanical block is not the approval's business.
	if _, blocked := plan.ReadBlock(dir, "photos", "upload"); !blocked {
		t.Errorf("plan approve must not clear a gate failure")
	}
}

func TestPlanApproveKeepsADeviationAgainstTheCurrentPlan(t *testing.T) {
	dir := blockedPlan(t)
	// Bind the deviation to the plan as it stands: nothing was revised, so nothing
	// is answered, and re-approving must not paper over the objection.
	hash, err := plan.HashPlan(filepath.Join(dir, "docs", "plans", "photos"))
	must(t, err)
	must(t, plan.WriteBlock(dir, "photos", plan.Block{
		Feat: "thumbs", Step: "spec-design", Kind: plan.BlockDeviation,
		Reason: "session blocked: needs a queue", PlanHash: hash,
	}))
	if code, _, e := run(t, "plan", "approve", "photos", "--root", dir); code != 0 {
		t.Fatalf("approve failed: %s", e)
	}
	if _, blocked := plan.ReadBlock(dir, "photos", "thumbs"); !blocked {
		t.Errorf("re-approving an unchanged plan resolves nothing; the deviation must stand")
	}
}

func TestPlanStatusShowsBlockKindAndTheWayOut(t *testing.T) {
	dir := blockedPlan(t)
	code, out, _ := run(t, "plan", "status", "photos", "--root", dir)
	if code != 0 {
		t.Fatalf("plan status failed: %d", code)
	}
	for _, want := range []string{"[gate-failure]", "[deviation]", "plan unblock photos --all"} {
		if !strings.Contains(out, want) {
			t.Errorf("status should show %q:\n%s", want, out)
		}
	}
}
