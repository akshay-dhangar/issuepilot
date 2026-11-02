package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akshay-dhangar/issuepilot/internal/agent"
	"github.com/akshay-dhangar/issuepilot/internal/httpapi"
	"github.com/akshay-dhangar/issuepilot/internal/model"
	"github.com/akshay-dhangar/issuepilot/internal/runner"
	"github.com/akshay-dhangar/issuepilot/internal/runs"
	"github.com/akshay-dhangar/issuepilot/internal/sandbox"
	"github.com/jackc/pgx/v5/pgxpool"
)

type scripted struct{}

func (scripted) Propose(ctx context.Context, r model.Request) (model.Proposal, error) {
	if r.Stage == "regression" {
		return model.Proposal{Summary: "scripted regression", Files: []model.File{{Path: "issuepilot_regression_test.go", Content: "package sample\nimport \"testing\"\nfunc TestIssuePilotRegression(t *testing.T){if Average(nil)!=0{t.Fatal(\"empty average\")}}\n"}}}, nil
	}
	return model.Proposal{Summary: "scripted fix", Files: []model.File{{Path: "average.go", Content: "package sample\nfunc Average(v []int)int{if len(v)==0{return 0};sum:=0;for _,x:=range v{sum+=x};return sum/len(v)}\n"}}}, nil
}
func TestDurableHTTPRepair(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if os.Getenv("TEST_DOCKER") != "1" || url == "" {
		t.Skip("TEST_DOCKER=1 and isolated TEST_DATABASE_URL required")
	}
	repo := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module fixture.local/api\n\ngo 1.25.0\n", "average.go": "package sample\nfunc Average(v []int)int{sum:=0;for _,x:=range v{sum+=x};return sum/len(v)}\n", "average_test.go": "package sample\nimport \"testing\"\nfunc TestAverage(t *testing.T){if Average([]int{2,4})!=3{t.Fatal(\"average\")}}\n"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", ".")
	git("-c", "user.name=IssuePilot Tests", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	sha := git("rev-parse", "HEAD")
	ctx := context.Background()
	db, err := runs.OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	token := strings.Repeat("a", 32)
	options := httpapi.Options{Token: token, RepairEnabled: true, Repositories: map[string]string{"sample": repo}, Model: "scripted-test-model"}
	server := httptest.NewServer(httpapi.HandlerWithOptions(db, options))
	defer server.Close()
	request := func(method, path, body string) (int, []byte) {
		t.Helper()
		r, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, data
	}
	status, body := request("POST", "/v1/repairs", `{"repository":"sample","commit":"`+sha+`","issue":"Average(nil) should return zero"}`)
	if status != 202 {
		t.Fatalf("submit: %d %s", status, body)
	}
	var job runs.Run
	if err = json.Unmarshal(body, &job); err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool, e := pgxpool.New(ctx, url)
		if e == nil {
			defer pool.Close()
			_, _ = pool.Exec(ctx, "DELETE FROM runs WHERE id=$1", job.ID)
		}
	}()
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	executor := runner.RepairExecutor{Engine: agent.Engine{Provider: scripted{}, Tester: sandbox.Docker{Image: sandbox.DefaultImage, Timeout: 2 * time.Minute}, MaxAttempts: 2}, Repositories: options.Repositories, Model: options.Model}
	go func() { done <- runner.Work(workerCtx, db, executor.Execute) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("worker did not stop")
		}
	}()
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		status, body = request("GET", "/v1/runs/"+job.ID, "")
		if status != 200 {
			t.Fatalf("poll: %d %s", status, body)
		}
		if err = json.Unmarshal(body, &job); err != nil {
			t.Fatal(err)
		}
		if job.Status == runs.AwaitingReview || job.Status == runs.Failed {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.Status != runs.AwaitingReview || len(job.Events) < 5 || len(job.Report) == 0 {
		t.Fatalf("repair status=%s error=%s events=%d", job.Status, job.Error, len(job.Events))
	}
	status, patch := request("GET", "/v1/runs/"+job.ID+"/patch", "")
	if status != 200 || !strings.Contains(string(patch), "issuepilot_regression_test.go") {
		t.Fatalf("patch: %d %s", status, patch)
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
		done <- nil
	case <-time.After(15 * time.Second):
		t.Fatal("worker shutdown")
	}
	server.Close()
	db.Close()
	db = nil
	// Reopen the persistent store and recover; completed reports must survive unchanged.
	db, err = runs.OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	persisted, err := db.Get(ctx, job.ID)
	if err != nil || persisted.Status != runs.AwaitingReview {
		t.Fatalf("report status after restart: %s %v", persisted.Status, err)
	}
	var before, after any
	if err = json.Unmarshal(job.Report, &before); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(persisted.Report, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("report contents changed after restart")
	}
	server = httptest.NewServer(httpapi.HandlerWithOptions(db, options))
	defer server.Close()
	status, again := request("GET", "/v1/runs/"+job.ID+"/patch", "")
	if status != 200 || string(again) != string(patch) {
		t.Fatal("patch changed after restart")
	}
	if git("status", "--porcelain") != "" {
		t.Fatal("user repository was modified")
	}
}
