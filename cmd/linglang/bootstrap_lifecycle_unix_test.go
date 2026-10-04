//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func testBootstrapRunLifetime(t *testing.T, cli string, env []string) {
	t.Helper()
	for _, program := range []struct {
		name  string
		body  string
		stdin bool
	}{
		{"cpu_loop", "for{}", false},
		{"blocked_stdin", `for{input:=readFile("/dev/stdin");assert(input.ok);if len(input.value)==0{break}}`, true},
	} {
		t.Run(program.name, func(t *testing.T) {
			for _, stop := range []struct {
				name   string
				signal syscall.Signal
				group  bool
			}{
				{"ctrl_c", syscall.SIGINT, true},
				{"terminate", syscall.SIGTERM, false},
				{"kill", syscall.SIGKILL, false},
			} {
				t.Run(stop.name, func(t *testing.T) {
					// The guarded launcher must preserve the same Unicode path for
					// startup, process discovery and cancellation cleanup.
					dir := filepath.Join(t.TempDir(), "雪 å's temporary directory")
					if err := os.Mkdir(dir, 0700); err != nil {
						t.Fatal(err)
					}
					source, ready := filepath.Join(dir, "loop.lang"), filepath.Join(dir, "ready")
					code := `package main
func main(){saved:=writeFile(head(args()).value,"ready");assert(saved.ok);` + program.body + `}`
					if err := os.WriteFile(source, []byte(code), 0600); err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, cli, "run", source, "--", ready)
					cmd.Env = append(append([]string{}, env...), "TMPDIR="+dir)
					// Isolate Ctrl+C from the test runner's own terminal group.
					cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
					cmd.WaitDelay = time.Second
					if program.stdin {
						input, writer, err := os.Pipe()
						if err != nil {
							t.Fatal(err)
						}
						// Keep the writer open until after process/cleanup assertions: EOF
						// must not release the read and conceal a cancellation deadlock.
						t.Cleanup(func() { input.Close(); writer.Close() })
						cmd.Stdin = input
					}
					var stdout, stderr bytes.Buffer
					cmd.Stdout, cmd.Stderr = &stdout, &stderr
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					finished := make(chan error, 1)
					go func() { finished <- cmd.Wait() }()
					waited := false
					t.Cleanup(func() {
						cancel()
						if !waited {
							<-finished
						}
						// A failing regression must not leave its program alive.
						for _, pid := range bootstrapRunProcesses(t, dir) {
							_ = syscall.Kill(pid, syscall.SIGKILL)
						}
					})
					deadline := time.Now().Add(10 * time.Second)
					for {
						if _, err := os.Stat(ready); err == nil {
							break
						}
						select {
						case err := <-finished:
							waited = true
							t.Fatalf("compiler exited before program started: %v\n%s", err, stderr.String())
						default:
						}
						if time.Now().After(deadline) {
							t.Fatal("program did not start")
						}
						time.Sleep(20 * time.Millisecond)
					}
					if pids := bootstrapRunProcesses(t, dir); len(pids) != 1 {
						t.Fatalf("expected one running child VM, got %v", pids)
					}
					if program.stdin {
						// The ready marker precedes the read; allow it to enter the file
						// server before interrupting the compiler.
						time.Sleep(100 * time.Millisecond)
					}
					target := cmd.Process.Pid
					if stop.group {
						target = -target
					}
					if err := syscall.Kill(target, stop.signal); err != nil {
						t.Fatal(err)
					}
					select {
					case <-finished:
						waited = true
					case <-time.After(3 * time.Second):
						t.Fatal("interrupted compiler did not exit")
					}
					deadline = time.Now().Add(2 * time.Second)
					for {
						pids := bootstrapRunProcesses(t, dir)
						if len(pids) == 0 {
							break
						}
						if time.Now().After(deadline) {
							t.Fatalf("orphaned program after %s: %v", stop.name, pids)
						}
						time.Sleep(20 * time.Millisecond)
					}
					leftovers, err := filepath.Glob(filepath.Join(dir, "linglang-run-*"))
					if err != nil || len(leftovers) != 0 {
						t.Fatalf("interrupted run left temporary artifacts: %v (%v)", leftovers, err)
					}
				})
			}
		})
	}
}

func bootstrapRunProcesses(t *testing.T, dir string) []int {
	t.Helper()
	out, err := exec.Command("ps", "-axo", "pid=,stat=,command=").Output()
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		var pid int
		var state string
		if _, err := fmt.Sscanf(line, "%d %s", &pid, &state); err != nil {
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), strconv.Itoa(pid)))
		command = strings.TrimSpace(strings.TrimPrefix(command, state))
		if !strings.HasPrefix(state, "Z") && strings.Contains(command, filepath.Join(dir, "linglang-run-")) {
			pids = append(pids, pid)
		}
	}
	return pids
}
