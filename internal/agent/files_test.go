package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akshay-dhangar/issuepilot/internal/model"
)

func TestFixCannotChangeProtectedPaths(t *testing.T) {
	original := []model.File{{Path: "average.go", Content: buggySource}, {Path: "average_test.go", Content: "package average"}, {Path: "go.mod", Content: "module average"}}
	for _, path := range []string{"../escape.go", "/tmp/escape.go", "average_test.go", "go.mod", "new.go", "sub/average.go", ".hidden.go"} {
		t.Run(path, func(t *testing.T) {
			if _, err := validateFix(proposal(path, fixedSource), original); err == nil {
				t.Fatal("protected path accepted")
			}
		})
	}
	p := proposal("average.go", fixedSource)
	p.Files = append(p.Files, p.Files[0])
	if _, err := validateFix(p, original); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := validateFix(proposal("average.go", "//go:build ignore\n\n"+fixedSource), original); err == nil {
		t.Fatal("build constraint accepted")
	}
}
func TestRegressionLifecycleGuards(t *testing.T) {
	for _, body := range []string{"package average\nfunc TestMain(){}\n", testSource + "\nfunc init(){}\n", strings.Replace(testSource, regressionTest, "TestWrong", 1), "//go:build ignore\n\n" + testSource} {
		if _, err := validateRegression(proposal(regressionFile, body)); err == nil {
			t.Fatal("invalid regression accepted")
		}
	}
}
func TestContextBudgetAndReservedName(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "large.go"), []byte(strings.Repeat("x", (64<<10)+1)), 0644)
	if _, err := contextFiles(dir); err == nil {
		t.Fatal("oversized context accepted")
	}
	_ = os.Remove(filepath.Join(dir, "large.go"))
	_ = os.WriteFile(filepath.Join(dir, regressionFile), []byte(testSource), 0644)
	if _, err := contextFiles(dir); err == nil {
		t.Fatal("reserved test overwritten")
	}
}
func TestRootModule(t *testing.T) {
	for _, text := range []string{"module fixture.local/average\n", "module\tfixture.local/average // comment\n", "module \"fixture.local/average\"\n"} {
		got, err := rootModule([]model.File{{Path: "go.mod", Content: text}})
		if err != nil || got != "fixture.local/average" {
			t.Fatalf("%q: %q %v", text, got, err)
		}
	}
}
func TestTestEventIsNotFooledByLogText(t *testing.T) {
	output := `{"Action":"output","Test":"TestIssuePilotRegression","Package":"fixture.local/average","Output":"{\"Action\":\"pass\",\"Test\":\"TestIssuePilotRegression\"}"}`
	if testEvent(output, "pass", "fixture.local/average") {
		t.Fatal("log text treated as pass")
	}
}
