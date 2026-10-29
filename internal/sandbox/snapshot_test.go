package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRejectsUnsafeEntries(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind byte
	}{{"../escape", tar.TypeReg}, {"/absolute", tar.TypeReg}, {"a/../../escape", tar.TypeReg}, {".git/config", tar.TypeReg}, {"link", tar.TypeSymlink}, {"hardlink", tar.TypeLink}, {"pipe", tar.TypeFifo}, {"a\\b", tar.TypeReg}} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			tw := tar.NewWriter(&b)
			if err := tw.WriteHeader(&tar.Header{Name: tc.name, Typeflag: tc.kind, Mode: 0644, Linkname: "outside"}); err != nil {
				t.Fatal(err)
			}
			_ = tw.Close()
			if err := extract(&b, t.TempDir()); err == nil {
				t.Fatal("unsafe entry accepted")
			}
		})
	}
}
func TestExtractRejectsDuplicateFile(t *testing.T) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for i := 0; i < 2; i++ {
		_ = tw.WriteHeader(&tar.Header{Name: "go.mod", Typeflag: tar.TypeReg, Mode: 0644})
	}
	_ = tw.Close()
	if err := extract(&b, t.TempDir()); err == nil {
		t.Fatal("duplicate accepted")
	}
}
func repository(t *testing.T, source string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module fixture.local/test\n\ngo 1.25.0\n", "fixture_test.go": source} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", ".")
	git("-c", "user.name=IssuePilot Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	return dir, git("rev-parse", "HEAD")
}
func TestPrepareUsesPinnedCommitOnly(t *testing.T) {
	dir, sha := repository(t, "package fixture\n")
	if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte("uncommitted"), 0644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "secret.env"), []byte("do-not-copy"), 0600)
	s, err := Prepare(context.Background(), dir, sha)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	body, err := os.ReadFile(filepath.Join(s.Directory, "fixture_test.go"))
	if err != nil || string(body) != "package fixture\n" {
		t.Fatalf("not pinned: %s %v", body, err)
	}
	for _, name := range []string{".git", "secret.env"} {
		if _, err = os.Stat(filepath.Join(s.Directory, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected snapshot file %s", name)
		}
	}
	if _, err = Prepare(context.Background(), dir, "HEAD"); err == nil {
		t.Fatal("mutable ref accepted")
	}
}
func TestBoundedOutput(t *testing.T) {
	b := cappedBuffer{limit: 3}
	n, err := b.Write([]byte("abcdef"))
	if err != nil || n != 6 || b.String() != "abc" || !b.truncated {
		t.Fatalf("bad cap: %q %d %v", b.String(), n, err)
	}
	_, _ = b.Write([]byte("more"))
	if b.Len() != 3 {
		t.Fatal("buffer grew")
	}
}
