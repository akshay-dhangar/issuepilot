// Package sandbox prepares immutable source snapshots and executes Go tests in Docker.
package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const maxSnapshotBytes = 32 << 20
const maxSnapshotFiles = 5000

var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

type Snapshot struct {
	Directory string
	Commit    string
}

func (s Snapshot) Close() error { return os.RemoveAll(s.Directory) }

// Prepare exports tracked files from an exact commit; uncommitted files and .git
// never enter the runner. Symlinks and special files are deliberately unsupported.
func Prepare(ctx context.Context, repository, commit string) (Snapshot, error) {
	if !commitPattern.MatchString(commit) {
		return Snapshot{}, errors.New("commit must be a full 40-character lowercase Git SHA")
	}
	repository, err := filepath.Abs(repository)
	if err != nil {
		return Snapshot{}, err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repository, "rev-parse", "--verify", commit+"^{commit}")
	result, err := cmd.Output()
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve commit: %w", err)
	}
	if strings.TrimSpace(string(result)) != commit {
		return Snapshot{}, errors.New("commit did not resolve to requested SHA")
	}
	cmd = exec.CommandContext(ctx, "git", "-C", repository, "archive", "--format=tar", commit)
	var archive cappedBuffer
	archive.limit = maxSnapshotBytes
	cmd.Stdout = &archive
	if err = cmd.Run(); err != nil {
		return Snapshot{}, fmt.Errorf("export commit: %w", err)
	}
	if archive.truncated {
		return Snapshot{}, errors.New("snapshot exceeds 32 MiB limit")
	}
	snapshotRoot := os.Getenv("ISSUEPILOT_SNAPSHOT_ROOT")
	if snapshotRoot == "" {
		working, e := os.Getwd()
		if e != nil {
			return Snapshot{}, e
		}
		snapshotRoot = filepath.Join(working, ".issuepilot", "snapshots")
	}
	if err := os.MkdirAll(snapshotRoot, 0700); err != nil {
		return Snapshot{}, err
	}
	dir, err := os.MkdirTemp(snapshotRoot, "run-")
	if err != nil {
		return Snapshot{}, err
	}
	// Readable by the unprivileged container user; only public/approved source belongs here.
	if err = os.Chmod(dir, 0755); err != nil {
		_ = os.RemoveAll(dir)
		return Snapshot{}, err
	}
	s := Snapshot{Directory: dir, Commit: commit}
	if err = extract(bytes.NewReader(archive.Bytes()), dir); err != nil {
		_ = s.Close()
		return Snapshot{}, err
	}
	info, err := os.Stat(filepath.Join(dir, "go.mod"))
	if err != nil || !info.Mode().IsRegular() {
		_ = s.Close()
		return Snapshot{}, errors.New("snapshot must contain a root go.mod")
	}
	return s, nil
}

func extract(reader io.Reader, dir string) error {
	tr := tar.NewReader(reader)
	var total int64
	files := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || path.IsAbs(name) || path.Clean(name) != name || strings.Contains(name, "\\") {
			return fmt.Errorf("unsafe archive path %q", h.Name)
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." || part == ".git" {
				return fmt.Errorf("forbidden archive path %q", h.Name)
			}
		}
		files++
		if files > maxSnapshotFiles {
			return errors.New("snapshot exceeds file count limit")
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if h.Size < 0 || h.Size > maxSnapshotBytes-total {
				return errors.New("snapshot exceeds size limit")
			}
			total += h.Size
			if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
			if e != nil {
				return e
			}
			_, e = io.CopyN(f, tr, h.Size)
			closeErr := f.Close()
			if e != nil {
				return e
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("unsupported archive entry %q (links and special files are prohibited)", h.Name)
		}
	}
}
