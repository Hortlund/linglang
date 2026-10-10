package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStructuredRunnerCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("JSON test driver currently targets Linux/macOS")
	}
	executable := filepath.Join(t.TempDir(), "linglang")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", executable, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	testStructuredDriver(t, executable, os.Environ(), "go-seed")
}

// Reused by both seed-built bootstrap modes and the actual generation C proof.
func testStructuredDriver(t *testing.T, executable string, env []string, driver string) {
	t.Helper()
	root := t.TempDir()
	write := func(path, source string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	invoke := func(args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("hung: %s", stderr.String())
		}
		return stdout.String(), stderr.String(), err
	}
	file := filepath.Join(root, "quoted \"雪\"\nmain.lang")
	for _, baseline := range []bool{false, true} {
		for _, fixture := range []struct{ source, code, marker string }{
			{"package main\nfunc main(){panic(\"must not run\")}", "", ""},
			{"package main\n//line imaginary.lang:999\nfunc main(){println(\"雪\"); @}", "LL1001", "@"},
			{"package main\n//line imaginary.lang:999\nfunc main(){println(\"雪\");var n int=\"wrong\";println(n)}", "LL1003", ""},
			{"package main\nconst Value=1", "LL1004", ""},
			{"package main\nimport \"./missing\"\nfunc main(){}", "LL1002", ""},
		} {
			write(file, fixture.source)
			args := []string{"check", "--json"}
			if baseline {
				args = append(args, "--no-opt")
			}
			args = append(args, file)
			stdout, stderr, err := invoke(args...)
			var report checkReport
			if json.Unmarshal([]byte(stdout), &report) != nil || report.SchemaVersion != 1 || report.Compiler != driver || report.Command != "check" || report.Diagnostics == nil || report.SourceFiles != 1 {
				t.Fatalf("invalid check JSON: %s\n%s (%v)", stdout, stderr, err)
			}
			if fixture.code == "" {
				if err != nil || !report.OK || len(report.Diagnostics) != 0 {
					t.Fatalf("valid check: %s\n%s (%v)", stdout, stderr, err)
				}
			} else {
				if err == nil || report.OK || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != fixture.code {
					t.Fatalf("want %s: %s\n%s (%v)", fixture.code, stdout, stderr, err)
				}
				at := report.Diagnostics[0].Location
				if at == nil || at.File != file || strings.Contains(stderr, "imaginary.lang") {
					t.Fatalf("lost physical location: %s\n%s", stdout, stderr)
				}
				if fixture.marker != "" && (at.Offset != strings.Index(fixture.source, fixture.marker) || at.Line != 3) {
					t.Fatalf("bad byte offset: %s", stdout)
				}
			}
		}
	}
	// Imported syntax failures retain their physical origin and import trail.
	leaf := filepath.Join(root, "leaf", "leaf.lang")
	write(leaf, "package leaf\nfunc Value()int{return @}")
	write(filepath.Join(root, "lib", "lib.lang"), "package lib\nimport \"../leaf\"\nfunc Value()int{return leaf.Value()}")
	write(file, "package main\nimport \"./lib\"\nfunc main(){println(lib.Value())}")
	out, stderr, err := invoke("check", "--json", file)
	var report checkReport
	if json.Unmarshal([]byte(out), &report) != nil || err == nil || len(report.Diagnostics) != 1 || report.Diagnostics[0].Location == nil || report.Diagnostics[0].Location.File != leaf || len(report.Diagnostics[0].Related) != 2 {
		t.Fatalf("import trail: %s\n%s (%v)", out, stderr, err)
	}
	out, stderr, err = invoke("check", "--json", filepath.Join(root, "absent"))
	if json.Unmarshal([]byte(out), &report) != nil || err == nil || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "LL1000" || report.Diagnostics[0].Location != nil {
		t.Fatalf("input failure: %s\n%s (%v)", out, stderr, err)
	}
	suite := filepath.Join(root, "suite")
	tests := filepath.Join(suite, "suite_test.lang")
	write(tests, "package library\nfunc TestPass(){println(\"test-output { not JSON }\");assert(true)}")
	out, stderr, err = invoke("check", "--json", "--tests", suite)
	if json.Unmarshal([]byte(out), &report) != nil || err != nil || !report.OK || report.Mode != "tests" {
		t.Fatalf("library check: %s\n%s (%v)", out, stderr, err)
	}
	for _, baseline := range []bool{false, true} {
		source := `package library
//line fake-test.lang:500
func TestA(){println("test-output { not JSON }");assert(false)}
func TestB(){for{}}
func TestZ(){println("last-test");assert(true)}`
		write(tests, source)
		args := []string{"test", "--json", "--gc-stress", "--gc-stats", "--timeout", "2s"}
		if baseline {
			args = append(args, "--no-opt")
		}
		args = append(args, suite)
		out, stderr, err = invoke(args...)
		events := decodeTestEvents(t, out, driver)
		if err == nil || len(events) != 8 || events[0]["event"] != "suite_start" || !strings.Contains(stderr, "test-output") || !strings.Contains(stderr, "last-test") {
			t.Fatalf("test sequence: %s\n%s (%v)", out, stderr, err)
		}
		for i, status := range []string{"fail", "timeout", "pass"} {
			start, end := events[1+i*2], events[2+i*2]
			if start["event"] != "test_start" || end["event"] != "test_end" || end["status"] != status || start["name"] != end["name"] || end["durationMillis"].(float64) < 0 {
				t.Fatalf("bad test pair: %+v %+v", start, end)
			}
			at := end["location"].(map[string]any)
			if at["file"] != tests || at["line"] != float64(3+i) || at["offset"] != float64(strings.Index(source, end["name"].(string))) {
				t.Fatalf("test location: %+v", at)
			}
		}
		end := events[7]
		if end["event"] != "suite_end" || end["ok"] != false || end["passed"] != float64(1) || end["failed"] != float64(2) || end["total"] != float64(3) {
			t.Fatalf("summary: %+v", end)
		}
	}
	for _, source := range []string{"package library\nfunc TestBad(n int){}", "package library\nfunc helper(){}", "package library\nfunc TestBad(){@}"} {
		write(tests, source)
		out, stderr, err = invoke("test", "--json", suite)
		events := decodeTestEvents(t, out, driver)
		if err == nil || len(events) != 2 || events[0]["event"] != "diagnostic" || events[1]["event"] != "suite_end" || events[1]["ok"] != false || events[1]["total"] != float64(0) {
			t.Fatalf("setup failure: %s\n%s (%v)", out, stderr, err)
		}
	}
	write(tests, "package library\nfunc TestSuccess(){println(\"ordinary output\");assert(writeFile(\"/dev/stdout\",\"descriptor-output\\n\").ok)}")
	out, stderr, err = invoke("test", "--json", suite)
	events := decodeTestEvents(t, out, driver)
	if err != nil || len(events) != 4 || events[3]["ok"] != true || events[3]["passed"] != float64(1) || (!strings.Contains(stderr, "ordinary output") || !strings.Contains(stderr, "descriptor-output")) {
		t.Fatalf("successful suite: %s\n%s (%v)", out, stderr, err)
	}
	for _, args := range [][]string{{"test", "--json", "--timeout", "0", suite}, {"check", "--tests"}, {"run", "--json", file}} {
		out, stderr, err = invoke(args...)
		if err == nil || out != "" || stderr == "" {
			t.Fatalf("invalid invocation: %s\n%s (%v)", out, stderr, err)
		}
	}
	if driver == "bootstrap" {
		t.Run("lifetime", func(t *testing.T) { testBootstrapTestLifetime(t, executable, env) })
	}
}

func decodeTestEvents(t *testing.T, output, driver string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil || event["schemaVersion"] != float64(1) || event["compiler"] != driver || event["command"] != "test" {
			t.Fatalf("invalid test event: %q (%v)", line, err)
		}
		events = append(events, event)
	}
	return events
}
