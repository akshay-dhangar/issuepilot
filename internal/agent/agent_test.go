package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akshay-dhangar/issuepilot/internal/model"
	"github.com/akshay-dhangar/issuepilot/internal/sandbox"
)

const buggySource = "package average\nfunc Average(v []int) int { sum:=0; for _,x:=range v {sum+=x}; return sum/len(v) }\n"
const fixedSource = "package average\nfunc Average(v []int) int { if len(v)==0{return 0}; sum:=0; for _,x:=range v {sum+=x}; return sum/len(v) }\n"
const testSource = "package average\nimport \"testing\"\nfunc TestIssuePilotRegression(t *testing.T){if Average(nil)!=0{t.Fatal(\"empty average must be zero\")}}\n"

func fixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module fixture.local/average\n\ngo 1.25.0\n", "average.go": buggySource, "average_test.go": "package average\nimport \"testing\"\nfunc TestExisting(t *testing.T){if Average([]int{2,4})!=3{t.Fatal(\"average\")}}\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", ".")
	git("-c", "user.name=IssuePilot Tests", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	return dir, git("rev-parse", "HEAD")
}

type scriptedProvider struct {
	proposals []model.Proposal
	requests  []model.Request
}

func (p *scriptedProvider) Propose(ctx context.Context, r model.Request) (model.Proposal, error) {
	p.requests = append(p.requests, r)
	if len(p.proposals) == 0 {
		return model.Proposal{}, fmt.Errorf("unexpected model call")
	}
	next := p.proposals[0]
	p.proposals = p.proposals[1:]
	return next, nil
}
func proposal(path, body string) model.Proposal {
	return model.Proposal{Summary: "scripted test response", Files: []model.File{{Path: path, Content: body}}}
}

type scriptedTester struct {
	results []sandbox.Result
	calls   int
}

func (p *scriptedTester) TestJSON(ctx context.Context, s sandbox.Snapshot) (sandbox.Result, error) {
	p.calls++
	if len(p.results) == 0 {
		return sandbox.Result{}, fmt.Errorf("unexpected test call")
	}
	r := p.results[0]
	p.results = p.results[1:]
	return r, nil
}
func evidence(code int, action string) sandbox.Result {
	return sandbox.Result{ExitCode: code, Output: fmt.Sprintf("{\"Action\":%q,\"Test\":%q,\"Package\":\"fixture.local/average\"}\n", action, regressionTest)}
}
func TestRepairLoopRetriesWithoutChangingTest(t *testing.T) {
	dir, sha := fixture(t)
	p := &scriptedProvider{proposals: []model.Proposal{proposal(regressionFile, testSource), proposal("average.go", "package average\nfunc Average(v []int)int{return 7}\n"), proposal("average.go", fixedSource)}}
	tester := &scriptedTester{results: []sandbox.Result{{ExitCode: 0}, evidence(1, "fail"), evidence(1, "fail"), evidence(0, "pass")}}
	r, err := (Engine{Provider: p, Tester: tester, MaxAttempts: 2}).Run(context.Background(), dir, sha, "Handle empty input")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "awaiting_review" || len(r.Attempts) != 2 || len(p.requests) != 3 || r.Patch == "" || r.PatchSHA256 == "" || r.RegressionSHA256 == "" {
		t.Fatalf("unexpected report: %+v", r)
	}
	frozen := p.requests[1].Files[len(p.requests[1].Files)-1]
	if frozen != p.requests[2].Files[len(p.requests[2].Files)-1] {
		t.Fatal("regression changed between attempts")
	}
	if !strings.Contains(p.requests[2].Feedback, "Previous candidate") {
		t.Fatal("missing repair feedback")
	}
	patch := filepath.Join(t.TempDir(), "patch.diff")
	_ = os.WriteFile(patch, []byte(r.Patch), 0600)
	if out, err := exec.Command("git", "-C", dir, "apply", "--check", patch).CombinedOutput(); err != nil {
		t.Fatalf("patch cannot apply: %v %s\n%s", err, out, r.Patch)
	}
	original, _ := os.ReadFile(filepath.Join(dir, "average.go"))
	if string(original) != buggySource {
		t.Fatal("user source modified")
	}
}
func TestReproductionMustBeActualRootTestFailure(t *testing.T) {
	for _, result := range []sandbox.Result{
		{ExitCode: 1, Output: "build failed"}, evidence(0, "pass"), evidence(1, "skip"), {ExitCode: 1, Output: `{"Action":"fail","Test":"TestIssuePilotRegression","Package":"other/package"}`},
	} {
		dir, sha := fixture(t)
		p := &scriptedProvider{proposals: []model.Proposal{proposal(regressionFile, testSource)}}
		tester := &scriptedTester{results: []sandbox.Result{{ExitCode: 0}, result}}
		r, err := (Engine{Provider: p, Tester: tester, MaxAttempts: 2}).Run(context.Background(), dir, sha, "fix")
		if err == nil || len(p.requests) != 1 || r.Patch != "" {
			t.Fatalf("invalid reproduction accepted: %+v %v", r, err)
		}
	}
}
func TestBaselineFailurePreventsModelCall(t *testing.T) {
	dir, sha := fixture(t)
	p := &scriptedProvider{}
	r, err := (Engine{Provider: p, Tester: &scriptedTester{results: []sandbox.Result{{ExitCode: 1}}}, MaxAttempts: 2}).Run(context.Background(), dir, sha, "fix")
	if err == nil || len(p.requests) != 0 || r.Status != "failed" {
		t.Fatal("baseline failure was ignored")
	}
}
func TestAttemptBudgetRejectsProtectedFiles(t *testing.T) {
	dir, sha := fixture(t)
	p := &scriptedProvider{proposals: []model.Proposal{proposal(regressionFile, testSource), proposal(regressionFile, "package average"), proposal("../escape.go", fixedSource)}}
	tester := &scriptedTester{results: []sandbox.Result{{ExitCode: 0}, evidence(1, "fail")}}
	r, err := (Engine{Provider: p, Tester: tester, MaxAttempts: 2}).Run(context.Background(), dir, sha, "fix")
	if err == nil || len(r.Attempts) != 2 || tester.calls != 2 || r.Patch != "" {
		t.Fatal("unsafe edits executed or budget exceeded")
	}
}
func TestDockerModelLoop(t *testing.T) {
	if os.Getenv("TEST_DOCKER") != "1" {
		t.Skip("TEST_DOCKER=1 required")
	}
	dir, sha := fixture(t)
	p := &scriptedProvider{proposals: []model.Proposal{proposal(regressionFile, testSource), proposal("average.go", fixedSource)}}
	r, err := (Engine{Provider: p, Tester: sandbox.Docker{Image: sandbox.DefaultImage, Timeout: 2 * time.Minute}, MaxAttempts: 2}).Run(context.Background(), dir, sha, "Average(nil) must return 0 without a panic")
	if err != nil {
		t.Fatalf("%v\n%+v", err, r)
	}
	if r.Status != "awaiting_review" || r.Patch == "" || r.PatchSHA256 == "" || r.RegressionSHA256 == "" {
		t.Fatalf("missing verified patch: %+v", r)
	}
	if !strings.Contains(r.Reproduction.Output, "integer divide by zero") {
		t.Fatal("real bug was not reproduced")
	}
	patch := filepath.Join(t.TempDir(), "patch.diff")
	_ = os.WriteFile(patch, []byte(r.Patch), 0600)
	if out, err := exec.Command("git", "-C", dir, "apply", patch).CombinedOutput(); err != nil {
		t.Fatalf("apply: %v %s", err, out)
	}
	// Verify the delivered artifact itself, not only the internal edited snapshot.
	if out, err := exec.Command("git", "-C", dir, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("stage patched fixture: %v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "-c", "user.name=IssuePilot Tests", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "Apply delivered patch").CombinedOutput(); err != nil {
		t.Fatalf("commit patched fixture: %v %s", err, out)
	}
	head, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	patched, err := sandbox.Prepare(context.Background(), dir, strings.TrimSpace(string(head)))
	if err != nil {
		t.Fatal(err)
	}
	defer patched.Close()
	verified, err := (sandbox.Docker{Image: sandbox.DefaultImage, Timeout: 2 * time.Minute}).TestJSON(context.Background(), patched)
	if err != nil || verified.ExitCode != 0 || !testEvent(verified.Output, "pass", "fixture.local/average") {
		t.Fatalf("delivered patch fails: %v %+v", err, verified)
	}
	out, err := exec.Command("git", "-C", dir, "diff", "HEAD~1", "--stat").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "average.go") {
		t.Fatalf("patch did not change source: %v %s", err, out)
	}
}

func TestProgressPersistenceFailureStopsModelWork(t *testing.T) {
	dir, sha := fixture(t)
	provider := &scriptedProvider{}
	tester := &scriptedTester{}
	engine := Engine{Provider: provider, Tester: tester, MaxAttempts: 2, Observe: func(string) error { return fmt.Errorf("storage unavailable") }}
	report, err := engine.Run(context.Background(), dir, sha, "fix")
	if err == nil || len(provider.requests) != 0 || tester.calls != 0 || report.Status != "failed" || report.Patch != "" {
		t.Fatal("work continued after failed progress persistence")
	}
}
