package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Set to a registry digest in image.go. Mutable image tags are never executed.
var imagePattern = regexp.MustCompile(`^golang@sha256:[a-f0-9]{64}$`)

type Result struct {
	Commit     string   `json:"commit"`
	Image      string   `json:"image"`
	Command    []string `json:"command"`
	ExitCode   int      `json:"exit_code"`
	Output     string   `json:"output"`
	Truncated  bool     `json:"output_truncated"`
	DurationMS int64    `json:"duration_ms"`
	TimedOut   bool     `json:"timed_out"`
}
type Docker struct {
	Image   string
	Timeout time.Duration
}

// Test runs a prepared snapshot. Only dependency-free or vendored Go modules
// work offline. Repository code never runs in a host process.
func (d Docker) Test(ctx context.Context, s Snapshot) (Result, error) {
	return d.test(ctx, s, false)
}

// TestJSON emits Go test events so callers can distinguish actual test failures
// from compilation errors and require the named regression to execute.
func (d Docker) TestJSON(ctx context.Context, s Snapshot) (Result, error) {
	return d.test(ctx, s, true)
}

func (d Docker) test(ctx context.Context, s Snapshot, jsonOutput bool) (result Result, err error) {
	if !imagePattern.MatchString(d.Image) {
		return result, errors.New("image must be golang@sha256:<64 lowercase hex characters>")
	}
	if d.Timeout <= 0 || d.Timeout > 5*time.Minute {
		return result, errors.New("timeout must be greater than zero and at most five minutes")
	}
	dir, e := filepath.EvalSymlinks(s.Directory)
	if e != nil {
		return result, e
	}
	dir, e = filepath.Abs(dir)
	if e != nil {
		return result, e
	}
	if strings.ContainsAny(dir, ",\n\r") {
		return result, errors.New("snapshot directory contains unsupported mount characters")
	}
	name := "issuepilot-" + strings.ToLower(rand.Text())
	result = Result{Commit: s.Commit, Image: d.Image, Command: []string{"go", "test", "-count=1", "-p=2", "-timeout=60s", "./..."}, ExitCode: -1}
	if jsonOutput {
		result.Command = append(result.Command[:2], append([]string{"-json"}, result.Command[2:]...)...)
	}
	started := time.Now()
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	// Create first, then start, so timeout cleanup cannot race container creation.
	createCtx, cancelCreate := context.WithTimeout(ctx, 20*time.Second)
	defer cancelCreate()
	args := containerArgs(name, dir, d.Image, result.Command)
	var creation cappedBuffer
	creation.limit = 16384
	cmd := exec.CommandContext(createCtx, "docker", args...)
	cmd.Stdout = &creation
	cmd.Stderr = &creation
	cmd.WaitDelay = time.Second
	if e = cmd.Run(); e != nil {
		cleanup(name) // also handles a daemon that created the container before CLI cancellation
		return result, fmt.Errorf("create runner (pull the pinned image first): %w: %s", e, creation.String())
	}
	defer func() {
		if e := cleanup(name); e != nil {
			err = errors.Join(err, fmt.Errorf("container cleanup: %w", e))
		}
	}()
	runCtx, cancel := context.WithTimeout(ctx, d.Timeout)
	defer cancel()
	var output cappedBuffer
	output.limit = 1 << 20
	cmd = exec.CommandContext(runCtx, "docker", "start", "--attach", name)
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.WaitDelay = time.Second
	e = cmd.Run()
	result.Output = output.String()
	result.Truncated = output.truncated
	if runCtx.Err() != nil {
		result.TimedOut = errors.Is(runCtx.Err(), context.DeadlineExceeded)
		return result, runCtx.Err()
	}
	// Inspect the actual container state; CLI failure is not a test failure.
	inspectCtx, inspectCancel := context.WithTimeout(ctx, 5*time.Second)
	defer inspectCancel()
	state, inspectErr := exec.CommandContext(inspectCtx, "docker", "inspect", "--format", "{{.State.Status}} {{.State.ExitCode}}", name).Output()
	if inspectErr != nil {
		return result, fmt.Errorf("inspect runner: %w", inspectErr)
	}
	var status string
	var code int
	if _, scanErr := fmt.Sscanf(string(state), "%s %d", &status, &code); scanErr != nil || status != "exited" {
		return result, fmt.Errorf("runner did not exit normally: %s (attach error: %v)", state, e)
	}
	result.ExitCode = code
	if e != nil && code == 0 {
		return result, fmt.Errorf("attach runner: %w", e)
	}
	return result, nil
}
func containerArgs(name, dir, image string, command []string) []string {
	args := []string{"create", "--pull=never", "--name", name, "--label", "app=issuepilot-runner", "--network=none", "--read-only", "--user=65534:65534", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--cpus=1", "--memory=768m", "--memory-swap=768m", "--ulimit=nofile=1024:1024", "--log-driver=none", "--tmpfs=/tmp:rw,exec,nosuid,nodev,size=536870912,mode=1777", "--mount", "type=bind,src=" + dir + ",dst=/workspace,readonly", "--workdir=/workspace", "--env=HOME=/tmp", "--env=GOCACHE=/tmp/go-build", "--env=GOPATH=/tmp/go", "--env=GOPROXY=off", "--env=GOSUMDB=off", "--env=GOTOOLCHAIN=local", "--env=CGO_ENABLED=0", "--env=GOMAXPROCS=2", "--env=GOWORK=off", image}
	return append(args, command...)
}
func cleanup(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "rm", "--force", name).Run()
}

type cappedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := b.limit - b.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
