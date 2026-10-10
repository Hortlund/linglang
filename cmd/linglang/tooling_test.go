package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

func TestStructuredToolingCLI(t *testing.T) {
	work := t.TempDir()
	binary := filepath.Join(work, "linglang")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	invoke := func(args ...string) (string, string, error) {
		t.Helper()
		command := exec.CommandContext(ctx, binary, args...)
		// These commands must work without OTP, Go, or the SQLite helper on PATH.
		command.Env = append(os.Environ(), "PATH="+t.TempDir())
		command.Dir = work
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		return stdout.String(), stderr.String(), err
	}
	file := filepath.Join(work, "quoted '雪' main.lang")
	if os.PathSeparator == '/' {
		file = filepath.Join(work, "quoted \"雪\"\nmain.lang")
	}
	for _, test := range []struct {
		name, source, code string
		args               []string
	}{
		{"valid", "package main\nfunc main(){panic(\"must not execute\")}", "", nil},
		{"syntax", "package main\nfunc main(){ @ }", compiler.CodeSyntax, nil},
		{"type", "package main\nfunc main(){println(\"雪\");missing()}", compiler.CodeType, nil},
		{"lowering", "package main\nfunc main(){defer println(1)}", compiler.CodeLanguage, nil},
		{"missing_main", "package main\nconst Value=1", compiler.CodeLanguage, nil},
		{"library", "package library\nfunc Value()int{return 1}", "", []string{"--tests"}},
		{"bad_test", "package library\nfunc TestBad(n int){}", compiler.CodeLanguage, []string{"--tests"}},
		{"import", "package main\nimport \"./missing\"\nfunc main(){}", compiler.CodeModule, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := file
			if test.name == "bad_test" {
				path = filepath.Join(work, "bad_test.lang")
			}
			if err := os.WriteFile(path, []byte(test.source), 0600); err != nil {
				t.Fatal(err)
			}
			for _, baseline := range []bool{false, true} {
				args := append([]string{"check", "--json"}, test.args...)
				if baseline {
					args = append(args, "--no-opt")
				}
				stdout, stderr, err := invoke(append(args, path)...)
				var report checkReport
				decoder := json.NewDecoder(strings.NewReader(stdout))
				decoder.DisallowUnknownFields()
				if decodeErr := decoder.Decode(&report); decodeErr != nil {
					t.Fatalf("JSON: %v\n%s\n%s", decodeErr, stdout, stderr)
				}
				var extra any
				if decoder.Decode(&extra) != io.EOF || stderr != "" || report.SchemaVersion != 1 || report.Compiler != "go-seed" || report.Command != "check" || report.SourceFiles != 1 || report.Diagnostics == nil {
					t.Fatalf("invalid report/streams: %s\n%s", stdout, stderr)
				}
				if test.code == "" {
					if err != nil || !report.OK || len(report.Diagnostics) != 0 {
						t.Fatalf("check: %v\n%s", err, stdout)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 1 || report.OK || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != test.code || report.Diagnostics[0].Location == nil || report.Diagnostics[0].Location.File != path {
						t.Fatalf("diagnostic/exit: %v\n%s", err, stdout)
					}
				}
				if len(test.args) > 0 && report.Mode != "tests" {
					t.Fatal("test check mode missing")
				}
			}
		})
	}
	stdout, stderr, err := invoke("check", "--json", filepath.Join(work, "absent.lang"))
	var report checkReport
	if json.Unmarshal([]byte(stdout), &report) != nil || err == nil || stderr != "" || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != compiler.CodeInput || report.Diagnostics[0].Location != nil {
		t.Fatalf("input error: %v\n%s\n%s", err, stdout, stderr)
	}
	stdout, stderr, err = invoke("describe", "--json")
	var description toolDescription
	if json.Unmarshal([]byte(stdout), &description) != nil || err != nil || stderr != "" || description.SchemaVersion != 1 || description.Compiler != "go-seed" || len(description.NativeAPI) == 0 {
		t.Fatalf("describe: %v\n%s\n%s", err, stdout, stderr)
	}
	second, _, _ := invoke("describe", "--json")
	if stdout != second {
		t.Fatal("description is nondeterministic")
	}
	text, _, err := invoke("describe")
	if err != nil || !strings.Contains(text, "func readFile(path string) TextResult") || !strings.Contains(text, "check [--json] [--tests]") {
		t.Fatalf("human discovery: %v\n%s", err, text)
	}
	for _, args := range [][]string{{"check", "--json"}, {"check", "--unknown", file}, {"describe", "unexpected"}} {
		stdout, stderr, err := invoke(args...)
		if err == nil || stdout != "" || stderr == "" {
			t.Fatalf("usage error: %v\n%s\n%s", err, stdout, stderr)
		}
	}
}

func TestCheckTestDiscovery(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{"main.lang": "package main\nfunc main(){}", "bad_test.lang": "package main\nfunc TestBad(n int){}"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if err := checkCommand([]string{dir}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if err := checkCommand([]string{"--tests", dir}, &stdout, &stderr); err == nil {
		t.Fatal("test signature not checked")
	}
}

type rejectedOutput struct{}

func (rejectedOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestToolingOutputFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "main.lang")
	if err := os.WriteFile(file, []byte("package main\nfunc main(){}"), 0600); err != nil {
		t.Fatal(err)
	}
	err := checkCommand([]string{"--json", file}, rejectedOutput{}, io.Discard)
	var reported reportedError
	if !errors.Is(err, io.ErrClosedPipe) || errors.As(err, &reported) {
		t.Fatalf("output failure hidden: %v", err)
	}
	if err := describeCommand([]string{"--json"}, rejectedOutput{}, io.Discard); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("output failure hidden: %v", err)
	}
}
