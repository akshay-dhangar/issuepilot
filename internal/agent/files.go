package agent

import (
	"errors"
	"fmt"
	"github.com/akshay-dhangar/issuepilot/internal/model"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const regressionFile = "issuepilot_regression_test.go"
const regressionTest = "TestIssuePilotRegression"

var filename = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*\.go$`)

func contextFiles(dir string) ([]model.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := []model.File{}
	total := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!filename.MatchString(name) && name != "go.mod") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("source context contains non-regular file")
		}
		if info.Size() > 64<<10 {
			return nil, fmt.Errorf("context file %s exceeds 64 KiB", name)
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(data) {
			return nil, errors.New("source must be UTF-8")
		}
		total += len(data)
		if total > 128<<10 || len(files) >= 40 {
			return nil, errors.New("root package exceeds context budget (128 KiB/40 files)")
		}
		if name == regressionFile || strings.Contains(string(data), "func "+regressionTest+"(") {
			return nil, errors.New("repository already uses reserved regression name")
		}
		files = append(files, model.File{Path: name, Content: string(data)})
	}
	hasProduction := false
	for _, f := range files {
		if strings.HasSuffix(f.Path, ".go") && !strings.HasSuffix(f.Path, "_test.go") {
			hasProduction = true
		}
	}
	if !hasProduction {
		return nil, errors.New("repair currently requires production Go files in the repository root")
	}
	return files, nil
}
func normalized(file model.File) (model.File, error) {
	if !filename.MatchString(file.Path) || len(file.Content) > 64<<10 {
		return model.File{}, errors.New("invalid file path or content size")
	}
	if strings.Contains(file.Content, "//go:") || strings.Contains(file.Content, "// +build") {
		return model.File{}, errors.New("compiler directives and build constraints are unsupported in proposals")
	}
	data, err := format.Source([]byte(file.Content))
	if err != nil {
		return model.File{}, errors.New("proposal is not valid Go syntax")
	}
	file.Content = string(data)
	return file, nil
}
func validateRegression(proposal model.Proposal) (model.File, error) {
	if len(proposal.Files) != 1 || proposal.Files[0].Path != regressionFile {
		return model.File{}, errors.New("regression proposal must add only issuepilot_regression_test.go")
	}
	f, err := normalized(proposal.Files[0])
	if err != nil {
		return f, err
	}
	tree, err := parser.ParseFile(token.NewFileSet(), f.Path, f.Content, 0)
	if err != nil {
		return f, err
	}
	count := 0
	for _, d := range tree.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			if fn.Name.Name == "init" || fn.Name.Name == "TestMain" {
				return f, errors.New("test lifecycle overrides are prohibited")
			}
			if strings.HasPrefix(fn.Name.Name, "Test") {
				if fn.Name.Name != regressionTest || fn.Recv != nil {
					return f, errors.New("only TestIssuePilotRegression is supported")
				}
				count++
			}
		}
	}
	if count != 1 {
		return f, errors.New("regression must declare TestIssuePilotRegression")
	}
	return f, nil
}
func validateFix(proposal model.Proposal, original []model.File) ([]model.File, error) {
	if len(proposal.Files) < 1 || len(proposal.Files) > 4 {
		return nil, errors.New("repair must replace 1-4 existing production Go files")
	}
	allowed := map[string]string{}
	for _, f := range original {
		if strings.HasSuffix(f.Path, ".go") && !strings.HasSuffix(f.Path, "_test.go") {
			allowed[f.Path] = f.Content
		}
	}
	seen := map[string]bool{}
	var edits []model.File
	for _, f := range proposal.Files {
		old, ok := allowed[f.Path]
		if !ok || seen[f.Path] {
			return nil, errors.New("repair targets a protected, unknown, or duplicate file")
		}
		seen[f.Path] = true
		if strings.Contains(old, "Code generated") {
			return nil, errors.New("generated source edits are unsupported")
		}
		f, err := normalized(f)
		if err != nil {
			return nil, err
		}
		if f.Content == old {
			continue
		}
		edits = append(edits, f)
	}
	if len(edits) == 0 {
		return nil, errors.New("repair makes no changes")
	}
	return edits, nil
}
func writeFiles(dir string, files []model.File) error {
	for _, f := range files {
		if !filename.MatchString(f.Path) {
			return errors.New("unsafe filename")
		}
		if err := os.WriteFile(filepath.Join(dir, f.Path), []byte(f.Content), 0644); err != nil {
			return err
		}
	}
	return nil
}

// fullPatch emits ordinary full-file unified hunks; bounded root filenames cannot inject headers.
func fullPatch(original []model.File, edits []model.File) string {
	old := map[string]string{}
	for _, f := range original {
		old[f.Path] = f.Content
	}
	var patch strings.Builder
	lines := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	}
	emit := func(prefix, body string) {
		for _, line := range lines(body) {
			patch.WriteString(prefix + line + "\n")
		}
		if body != "" && !strings.HasSuffix(body, "\n") {
			patch.WriteString("\\ No newline at end of file\n")
		}
	}
	for _, f := range edits {
		before, exists := old[f.Path]
		a := lines(before)
		b := lines(f.Content)
		from := "a/" + f.Path
		start := 1
		if !exists {
			from = "/dev/null"
		}
		if len(a) == 0 {
			start = 0
		}
		toStart := 1
		if len(b) == 0 {
			toStart = 0
		}
		fmt.Fprintf(&patch, "--- %s\n+++ b/%s\n@@ -%d,%d +%d,%d @@\n", from, f.Path, start, len(a), toStart, len(b))
		emit("-", before)
		emit("+", f.Content)
	}
	return patch.String()
}
