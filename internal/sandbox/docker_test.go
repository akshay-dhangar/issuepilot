package sandbox

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRejectMutableImage(t *testing.T) {
	_, err := (Docker{Image: "golang:latest", Timeout: time.Minute}).Test(context.Background(), Snapshot{})
	if err == nil {
		t.Fatal("mutable image accepted")
	}
}
func TestDockerIsolationAndFailure(t *testing.T) {
	if os.Getenv("TEST_DOCKER") != "1" {
		t.Skip("TEST_DOCKER=1 required")
	}
	for _, tc := range []struct {
		name, source string
		want         int
	}{
		{"isolation", `package fixture
import("net";"os";"testing";"time";"strings")
func TestIsolation(t *testing.T){
 if os.Getuid()==0{t.Fatal("running as root")}
 if os.Getenv("ISSUEPILOT_TEST_SECRET")!=""{t.Fatal("host secret leaked")}
 if _,err:=os.Stat("/var/run/docker.sock");!os.IsNotExist(err){t.Fatal("Docker socket visible")}
 if err:=os.WriteFile("/workspace/injected",[]byte("bad"),0600);err==nil{t.Fatal("writable source")}
 if err:=os.WriteFile("/injected",[]byte("bad"),0600);err==nil{t.Fatal("writable root")}
 conn,err:=net.DialTimeout("tcp","1.1.1.1:443",time.Second);if err==nil{conn.Close();t.Fatal("external network available")}
 status,err:=os.ReadFile("/proc/self/status");if err!=nil||!strings.Contains(string(status),"NoNewPrivs:\t1"){t.Fatal("privilege escalation not blocked")}
}
`, 0},
		{"failure", "package fixture\nimport \"testing\"\nfunc TestFails(t *testing.T){t.Fatal(\"expected failing test\")}\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ISSUEPILOT_TEST_SECRET", "must-not-enter-container")
			dir, sha := repository(t, tc.source)
			s, err := Prepare(context.Background(), dir, sha)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			result, err := (Docker{Image: DefaultImage, Timeout: 2 * time.Minute}).Test(context.Background(), s)
			if err != nil {
				t.Fatalf("%v\n%s", err, result.Output)
			}
			if tc.name == "failure" && !strings.Contains(result.Output, "expected failing test") {
				t.Fatalf("failed for an unexpected reason: %s", result.Output)
			}
			if result.ExitCode != tc.want {
				t.Fatalf("exit %d want %d: %s", result.ExitCode, tc.want, result.Output)
			}
		})
	}
}
func TestDockerTimeoutRemovesContainer(t *testing.T) {
	if os.Getenv("TEST_DOCKER") != "1" {
		t.Skip("TEST_DOCKER=1 required")
	}
	dir, sha := repository(t, "package fixture\nimport(\"testing\";\"time\")\nfunc TestSlow(t *testing.T){time.Sleep(time.Minute)}\n")
	s, err := Prepare(context.Background(), dir, sha)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := (Docker{Image: DefaultImage, Timeout: 500 * time.Millisecond}).Test(context.Background(), s)
	if err == nil || !r.TimedOut {
		t.Fatalf("expected timeout: %+v %v", r, err)
	}
	out, err := exec.Command("docker", "ps", "-aq", "--filter", "label=app=issuepilot-runner").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("runner container leaked: %s", out)
	}
}
