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
)

func TestDeveloperCommands(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "linglang")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	write := func(dir, name, data string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0640); err != nil {
			t.Fatal(err)
		}
		return path
	}
	invoke := func(dir string, noOTP bool, args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = dir
		if noOTP {
			for _, env := range os.Environ() {
				if !strings.HasPrefix(env, "PATH=") {
					cmd.Env = append(cmd.Env, env)
				}
			}
			cmd.Env = append(cmd.Env, "PATH="+t.TempDir())
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("CLI hung: %v\n%s\n%s", args, stdout.String(), stderr.String())
		}
		return stdout.String(), stderr.String(), err
	}
	t.Run("test_discovery_and_isolation", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "beans.lang", `package main
func main() { panic("main must not run") }
func increment(n int) int { return n + 1 }
func TestProductionHelper() { panic("not a test") }
func delayed(n int) { receive[int](600); writeFile("leaked.txt", "worker survived") }
`)
		write(dir, "beans_test.lang", `package main
func TestAWorker() { spawn(delayed, 0); assert(increment(1) == 2) }
func TestBNoLeak() { receive[int](800); assert(!readFile("leaked.txt").ok) }
func Test雪() { n := 1; p := &n; *p++; assert(n == 2) }
`)
		for _, baseline := range []bool{false, true} {
			args := []string{"test", "--gc-stress", "--gc-stats"}
			if baseline {
				args = append(args, "--no-opt")
			}
			stdout, stderr, err := invoke(dir, false, args...)
			want := "RUN TestAWorker\nPASS TestAWorker\nRUN TestBNoLeak\nPASS TestBNoLeak\nRUN Test雪\nPASS Test雪\nTests: 3 passed, 0 failed\n"
			if err != nil || stdout != want || strings.Count(stderr, "root_frames => 0") != 3 {
				t.Fatalf("test baseline=%t: %v\n%s\n%s", baseline, err, stdout, stderr)
			}
		}
	})
	t.Run("failures_timeout_and_continue", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "helper.lang", "package main\nfunc fail() { assert(false) }\n")
		write(dir, "beans_test.lang", `package main
func TestAAssert() { fail() }
func TestBPanic() { panic("bean exploded") }
func crash(n int) { panic("supervisor test") }
func TestCSupervisorExit() { s := startSupervisor(0, 5); supervise(s, "crash", crash, 0, ChildOptions{}); receive[int](-1) }
func TestDHang() { receive[int](-1) }
func TestEStillRuns() { assert(true) }
`)
		stdout, stderr, err := invoke(dir, false, "test", "--timeout", "1s")
		for _, want := range []string{"FAIL TestAAssert", "FAIL TestBPanic", "FAIL TestCSupervisorExit", "timeout after 1s", "PASS TestEStillRuns", "Tests: 1 passed, 4 failed"} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("missing %q: %s", want, stdout)
			}
		}
		if err == nil || !strings.Contains(stderr, "helper.lang:2: assertion failed") || !strings.Contains(stderr, "bean exploded") || !strings.Contains(stderr, "linglang process exited: shutdown") || !strings.Contains(stderr, "test suite failed") {
			t.Fatalf("failures lost: %v\n%s\n%s", err, stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(dir, "erl_crash.dump")); !os.IsNotExist(err) {
			t.Fatalf("unexpected crash dump: %v", err)
		}
	})
	t.Run("test_validation_and_normal_commands", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "main.lang", "package main\nfunc main() { println(42) }\n")
		testFile := write(dir, "broken_test.lang", "not a program")
		if out, diagnostics, err := invoke(dir, true, "check", "."); err != nil || out != "Checked . (1 source files)\n" || diagnostics != "" {
			t.Fatalf("check included tests: %v\n%s\n%s", err, out, diagnostics)
		}
		if out, diagnostics, err := invoke(dir, false, "run", "."); err != nil || out != "42\n" || diagnostics != "" {
			t.Fatalf("run included tests: %v\n%s\n%s", err, out, diagnostics)
		}
		if _, diagnostics, err := invoke(dir, true, "test"); err == nil || !strings.Contains(diagnostics, "broken_test.lang") {
			t.Fatalf("test skipped broken source: %v %s", err, diagnostics)
		}
		write(dir, "broken_test.lang", "package main\nfunc TestBad(n int) {}")
		if _, diagnostics, err := invoke(dir, true, "test"); err == nil || !strings.Contains(diagnostics, "test functions must have no") {
			t.Fatalf("test signature accepted: %v %s", err, diagnostics)
		}
		write(dir, "broken_test.lang", "package main\nfunc helper() {}")
		if _, diagnostics, err := invoke(dir, true, "test"); err == nil || !strings.Contains(diagnostics, "no Test functions") {
			t.Fatalf("empty suite accepted: %v %s", err, diagnostics)
		}
		write(dir, "broken_test.lang", "package main\nfunc TestStandalone() { assert(true) }")
		write(dir, "unrelated.lang", "broken sibling")
		if out, diagnostics, err := invoke(dir, false, "test", testFile); err != nil || !strings.Contains(out, "PASS TestStandalone") {
			t.Fatalf("explicit test file loaded sibling or required main: %v %s %s", err, out, diagnostics)
		}
		for _, timeout := range []string{"0", "-1s"} {
			if _, diagnostics, err := invoke(dir, true, "test", "--timeout", timeout); err == nil || !strings.Contains(diagnostics, "timeout must be positive") {
				t.Fatalf("timeout accepted: %v %s", err, diagnostics)
			}
		}
	})
	t.Run("formatter_check_permissions_links_and_idempotence", func(t *testing.T) {
		dir := t.TempDir()
		raw := "package main\n// bean\nfunc main(){println(1)}\n"
		path := write(dir, "main.lang", raw)
		write(dir, "nested/beans_test.lang", "package main\nfunc TestBean(){assert(true)}\n")
		for _, generated := range []string{".git", "_build", "_release", "bin", "dist"} {
			write(dir, generated+"/broken.lang", "not source")
		}
		link := filepath.Join(dir, "linked.lang")
		if err := os.Symlink("main.lang", link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(".", filepath.Join(dir, "cycle.lang")); err != nil {
			t.Fatal(err)
		}
		out, _, err := invoke(dir, true, "fmt", "--check", ".")
		if err == nil || strings.Count(out, ".lang\n") != 2 {
			t.Fatalf("fmt check: %v %s", err, out)
		}
		if data, _ := os.ReadFile(path); string(data) != raw {
			t.Fatal("fmt --check changed source")
		}
		out, diagnostics, err := invoke(dir, true, "fmt", ".", link, path)
		if err != nil || diagnostics != "" || strings.Count(out, ".lang\n") != 2 {
			t.Fatalf("fmt duplicate inputs: %v %s %s", err, out, diagnostics)
		}
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), "// bean\nfunc main() { println(1) }") {
			t.Fatalf("lost comment/source: %q %v", data, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0640 {
			t.Fatalf("lost permissions: %v %v", info, err)
		}
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("replaced symlink: %v %v", info, err)
		}
		if out, diagnostics, err := invoke(dir, true, "fmt", "--check", "."); err != nil || out != "" || diagnostics != "" {
			t.Fatalf("fmt not idempotent: %v %s %s", err, out, diagnostics)
		}
		write(dir, "main.lang", raw)
		if _, diagnostics, err := invoke(dir, true, "fmt", link); err != nil {
			t.Fatalf("explicit symlink: %v %s", err, diagnostics)
		}
		if data, _ := os.ReadFile(path); string(data) == raw {
			t.Fatal("explicit symlink did not format target")
		}
		if out, diagnostics, err := invoke(dir, false, "run", path); err != nil || out != "1\n" {
			t.Fatalf("formatted source failed: %v %s %s", err, out, diagnostics)
		}
	})
	t.Run("formatter_parse_before_write", func(t *testing.T) {
		dir := t.TempDir()
		raw := "package main\nfunc main(){}"
		path := write(dir, "a.lang", raw)
		write(dir, "z.lang", "package main\nfunc broken(")
		if _, diagnostics, err := invoke(dir, true, "fmt"); err == nil || !strings.Contains(diagnostics, "z.lang:2:") {
			t.Fatalf("syntax error lost: %v %s", err, diagnostics)
		}
		if data, _ := os.ReadFile(path); string(data) != raw {
			t.Fatal("syntax error left earlier source rewritten")
		}
		if _, diagnostics, err := invoke(t.TempDir(), true, "fmt"); err == nil || !strings.Contains(diagnostics, "no .lang files") {
			t.Fatalf("empty fmt accepted: %v %s", err, diagnostics)
		}
		if _, err := os.Stat("/dev/null"); err == nil {
			if _, diagnostics, err := invoke(dir, true, "fmt", "/dev/null"); err == nil || !strings.Contains(diagnostics, "regular file") {
				t.Fatalf("nonregular format accepted: %v %s", err, diagnostics)
			}
		}
	})
}
