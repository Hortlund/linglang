package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testBootstrapRunner(t *testing.T, executable string, env []string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package main\n"+source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("app.lang", `func main(){panic("main must not run")};func TestHelper(){panic("not a test file")};func worker(n int){receive[int](-1)}`)
	write("app_test.lang", `func TestA(){send(self(),1);spawn(worker,0)};func TestB(){assert(!receive[int](0).ok)}`)
	invoke := func(args ...string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("runner hung: %s", out)
		}
		return string(out), err
	}
	for _, mode := range [][]string{{"test", "--gc-stress", "--gc-stats", dir}, {"test", "--no-opt", dir}} {
		out, err := invoke(mode...)
		if err != nil || !strings.Contains(out, "Tests: 2 passed, 0 failed") {
			t.Fatalf("test success: %v\n%s", err, out)
		}
	}
	write("app_test.lang", `func TestA(){panic("expected failure")};func TestB(){spawn(worker,0);for{}};func TestZ(){assert(!receive[int](0).ok)}`)
	out, err := invoke("test", "--timeout", "500ms", dir)
	for _, want := range []string{"FAIL TestA", "FAIL TestB", ": timeout", "PASS TestZ", "Tests: 1 passed, 2 failed"} {
		if err == nil || !strings.Contains(out, want) {
			t.Fatalf("missing %q: %v\n%s", want, err, out)
		}
	}
	for i, source := range []string{`func TestBad(n int){}`, `func TestBad()int{return 0}`, `func helper(){}`} {
		write("app_test.lang", source)
		out, err := invoke("test", dir)
		if err == nil {
			t.Fatalf("accepted invalid suite %d: %s", i, out)
		}
	}
	write("app_test.lang", `func TestOnly(){}`)
	if err := os.Remove(filepath.Join(dir, "app.lang")); err != nil {
		t.Fatal(err)
	}
	out, err = invoke("test", dir)
	if err != nil || !strings.Contains(out, "PASS TestOnly") {
		t.Fatalf("mainless suite: %v\n%s", err, out)
	}
	for _, duration := range []string{"0", "-1", "2147483648", "99999999999999999999s", "nonsense"} {
		out, err = invoke("test", "--timeout", duration, dir)
		if err == nil {
			t.Fatal(fmt.Sprintf("accepted timeout %s: %s", duration, out))
		}
	}
}
