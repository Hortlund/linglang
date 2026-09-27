package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

func TestExecutionWithGCFlags(t *testing.T) {
	for _, tc := range []struct {
		name, source, stdout, diagnostic string
		fails                            bool
	}{
		{"success", "func main() { n := 41; p := &n; *p += 1; println(n) }", "42\n", "", false},
		{"runtime_error", "func main() { var p *int; println(*p) }", "", "linglang_nil_pointer", true},
		{"supervisor_cleanup", "func idle(n int) { receive[int](-1) }; func main() { s := startSupervisor(3, 5); c := supervise(s, \"idle\", idle, 0, ChildOptions{}); m := monitor(c.pid); stopSupervisor(s); result := wait(m, 5000); println(result.reason) }", "shutdown\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program, err := compiler.Compile("test.lang", []byte("package main\n"+tc.source))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := build(dir, program); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "erl", "-noshell", "-pa", dir, "-eval", evaluationScript(true, true))
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err = cmd.Run()
			if (err != nil) != tc.fails {
				t.Fatalf("unexpected exit: %v\n%s", err, stderr.String())
			}
			if stdout.String() != tc.stdout {
				t.Fatalf("stdout: got %q, want %q", stdout.String(), tc.stdout)
			}
			for _, want := range []string{"linglang GC:", "live_cells => 0", "root_frames => 0", tc.diagnostic} {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr missing %q:\n%s", want, stderr.String())
				}
			}
		})
	}
}

func TestSupervisorEscalationExitsCleanly(t *testing.T) {
	program, err := compiler.Compile("crash.lang", []byte(`package main
func crash(n int) { panic("restart limit test") }
func main() {
    s := startSupervisor(0, 5)
    supervise(s, "crash", crash, 0, ChildOptions{})
    receive[int](-1)
}`))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := build(dir, program); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "erl", "-noshell", "-pa", dir, "-eval", evaluationScript(true, true))
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if err == nil || !strings.Contains(stderr.String(), "linglang process exited: shutdown") {
		t.Fatalf("exit=%v; stdout=%s; stderr=%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "linglang GC: unavailable") {
		t.Fatal(stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "erl_crash.dump")); !os.IsNotExist(err) {
		t.Fatalf("unexpected VM crash dump: %v", err)
	}
}
