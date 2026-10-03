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
	"unicode/utf8"

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

func TestPrimeLabCLI(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "linglang")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	source, err := filepath.Abs("../../examples/prime_lab.lang")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("literal_arguments", func(t *testing.T) {
		program := filepath.Join(dir, "arguments.lang")
		if err := os.WriteFile(program, []byte("package main\nfunc main() { for _, value := range args() { println(value) } }"), 0600); err != nil {
			t.Fatal(err)
		}
		arguments := []string{"run", program, "雪 with spaces", "--gc-stress", "-extra", "\"quotes\";$(literal)"}
		output, err := exec.CommandContext(ctx, binary, arguments...).CombinedOutput()
		want := "雪 with spaces\n--gc-stress\n-extra\n\"quotes\";$(literal)\n"
		if err != nil || string(output) != want {
			t.Fatalf("literal arguments: got %q (%v), want %q", output, err, want)
		}
	})
	for _, target := range []struct{ name, path string }{
		{"single_file", source},
		{"directory", strings.TrimSuffix(source, ".lang")},
	} {
		source := target.path
		for _, baseline := range []bool{false, true} {
			for _, empty := range []bool{false, true} {
				name := "optimized"
				if baseline {
					name = "baseline"
				}
				if empty {
					name += "_empty"
				}
				t.Run(target.name+"_"+name, func(t *testing.T) {
					work := t.TempDir()
					input := filepath.Join(work, "雪 jobs.txt")
					output := filepath.Join(work, "雪 report.csv")
					jobs := "# test batch\r\n100\r\n\xff\r\n 300 # inline comment\r\nnope\n1\n100001\n9223372036854775808\n\xe2\x82\n10\n"
					want := "job,limit,prime_count,largest_prime\n1,100,25,97\n2,300,62,293\n3,10,4,7\n"
					completed := "Completed: 3 | Worker restarts: 1"
					if empty {
						jobs = "# no jobs\n \t\r\n"
						want = "job,limit,prime_count,largest_prime\n"
						completed = "Completed: 0 | Worker restarts: 0"
					}
					if err := os.WriteFile(input, []byte(jobs), 0600); err != nil {
						t.Fatal(err)
					}
					// Existing reports must be truncated, including an empty batch.
					if err := os.WriteFile(output, []byte(strings.Repeat("old report", 100)), 0600); err != nil {
						t.Fatal(err)
					}
					arguments := []string{"run", "--gc-stress", "--gc-stats"}
					if baseline {
						arguments = append(arguments, "--no-opt")
					}
					arguments = append(arguments, source, input, output)
					runCtx, runCancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer runCancel()
					cmd := exec.CommandContext(runCtx, binary, arguments...)
					cmd.Dir = work
					var stdout, stderr bytes.Buffer
					cmd.Stdout, cmd.Stderr = &stdout, &stderr
					if err := cmd.Run(); err != nil {
						t.Fatalf("run prime lab: %v\n%s\n%s", err, stdout.String(), stderr.String())
					}
					messages := []string{completed, "Supervisor stopped: true", "Saved CSV: " + output}
					if !empty {
						messages = append(messages, "Loaded jobs: 3 | Skipped: 6", "Pool workers: 3 | Peak active: 3",
							"Skipping line 3 (expected an integer from 2 to 100000)",
							"Skipping line 9 (expected an integer from 2 to 100000)")
					} else {
						messages = append(messages, "Pool workers: 3 | Peak active: 0")
					}
					if !utf8.Valid(stdout.Bytes()) {
						t.Fatalf("diagnostics contain malformed UTF-8: %q", stdout.Bytes())
					}
					for _, message := range messages {
						if !strings.Contains(stdout.String(), message) {
							t.Fatalf("missing %q:\n%s", message, stdout.String())
						}
					}
					for _, message := range []string{"live_cells => 0", "root_frames => 0"} {
						if !strings.Contains(stderr.String(), message) {
							t.Fatalf("missing %q:\n%s", message, stderr.String())
						}
					}
					report, err := os.ReadFile(output)
					if err != nil || string(report) != want {
						t.Fatalf("report: got %q (%v), want %q", report, err, want)
					}
				})
			}
		}
	}
	t.Run("file_failures", func(t *testing.T) {
		input := filepath.Join(dir, "valid.txt")
		if err := os.WriteFile(input, []byte("10\n"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			args   []string
			reason string
		}{
			{[]string{filepath.Join(dir, "absent.txt")}, "cannot read"},
			{[]string{input, filepath.Join(dir, "absent", "report.csv")}, "cannot write"},
			{[]string{input, "report.csv", "extra"}, "usage: prime_lab.lang"},
		} {
			runCtx, runCancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer runCancel()
			arguments := append([]string{"run", "--gc-stress", "--gc-stats", source}, tc.args...)
			cmd := exec.CommandContext(runCtx, binary, arguments...)
			cmd.Dir = dir
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), tc.reason) || !strings.Contains(string(output), "live_cells => 0") || !strings.Contains(string(output), "root_frames => 0") {
				t.Fatalf("expected %q and clean exit: %v\n%s", tc.reason, err, output)
			}
		}
	})
}
