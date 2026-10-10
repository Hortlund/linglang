package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

func testBootstrapDiscovery(t *testing.T, cli string, env []string) {
	t.Helper()
	work := t.TempDir()
	invoke := func(args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, cli, args...)
		cmd.Env, cmd.Dir = env, work
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("command timed out: %v", args)
		}
		// Seed-packed reference drivers intentionally emit GC statistics.
		clean := regexp.MustCompile(`(?s)linglang GC: #\{[^}]*\}\n`).ReplaceAllString(stderr.String(), "")
		return out.String(), clean, err
	}
	decode := func(text string, into any) {
		t.Helper()
		d := json.NewDecoder(strings.NewReader(text))
		d.DisallowUnknownFields()
		if err := d.Decode(into); err != nil {
			t.Fatalf("invalid JSON: %v %s", err, text)
		}
		if _, err := d.Token(); err != io.EOF {
			t.Fatalf("trailing JSON: %s", text)
		}
	}
	out, stderr, err := invoke("describe", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("describe: %v %s", err, stderr)
	}
	var description toolDescription
	decode(out, &description)
	if description.SchemaVersion != 1 || description.Compiler != "bootstrap" || description.Stability != "experimental" {
		t.Fatal(out)
	}
	again, _, _ := invoke("describe", "--json")
	if out != again {
		t.Fatal("nondeterministic description")
	}
	var commands []string
	for _, command := range description.Commands {
		commands = append(commands, command.Name)
	}
	sort.Strings(commands)
	if strings.Join(commands, ",") != "build,check,deps,describe,emit,fmt,init,run,test" {
		t.Fatal(commands)
	}
	expected, err := compiler.NativeAPI()
	if err != nil {
		t.Fatal(err)
	}
	normalize := func(n compiler.NativeDeclaration) string {
		t.Helper()
		if n.Kind == "opaque" || n.Kind == "collection" {
			return n.Declaration
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "native.lang", "package main\n"+n.Declaration, 0)
		if err != nil {
			t.Fatalf("bad native declaration %s: %v", n.Name, err)
		}
		var b bytes.Buffer
		if err := format.Node(&b, fset, f); err != nil {
			t.Fatal(err)
		}
		// Record fields may use semicolons or newlines; AST formatting respects
		// original line breaks, so compare token streams rather than indentation.
		return strings.Join(strings.Fields(strings.ReplaceAll(b.String(), ";", "")), "")
	}
	if len(expected) != len(description.NativeAPI) {
		t.Fatalf("native count %d != %d", len(expected), len(description.NativeAPI))
	}
	for i, want := range expected {
		got := description.NativeAPI[i]
		if want.Name != got.Name || want.Kind != got.Kind || normalize(want) != normalize(got) {
			t.Fatalf("native drift: %#v != %#v", want, got)
		}
	}
	out, stderr, err = invoke("describe")
	if err != nil || stderr != "" || !strings.Contains(out, "func readFile(path string) TextResult") || !strings.Contains(out, "deps [--check] [--json]") {
		t.Fatalf("human discovery: %v %s %s", err, out, stderr)
	}
	for _, args := range [][]string{{"describe", "bad"}, {"describe", "--bad"}, {"deps", "--json", "--bad"}, {"deps", "--json", "a", "b"}} {
		out, stderr, err := invoke(args...)
		if err == nil || out != "" || stderr == "" {
			t.Fatalf("usage %v: %v %s %s", args, err, out, stderr)
		}
	}

	root := filepath.Join(work, "app")
	lib := filepath.Join(root, "lib")
	shared := filepath.Join(work, "shared 雪💡")
	for _, dir := range []string{lib, shared} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sharedFile := filepath.Join(shared, "value.lang")
	write(filepath.Join(root, "main.lang"), "package main\nimport \"./lib\"\nfunc main(){panic(lib.Answer())}")
	write(filepath.Join(root, "ignored_test.lang"), "this test must not be parsed")
	write(filepath.Join(lib, "lib.lang"), "package lib\nimport shared \"../../shared 雪💡\"\nfunc Answer()int{return shared.Value()}")
	write(filepath.Join(lib, "extra \"quoted\".lang"), "package lib\nconst Extra=1")
	write(filepath.Join(lib, "ignored_test.lang"), "this dependency test must not be parsed")
	write(sharedFile, "package shared\nfunc Value()int{return 42}")
	target := filepath.Join(root, "linglang.lock")
	dependencyCall := func(check, success bool) dependencyReport {
		t.Helper()
		args := []string{"deps", "--json"}
		if check {
			args = append(args, "--check")
		}
		args = append(args, root)
		out, stderr, err := invoke(args...)
		var r dependencyReport
		decode(out, &r)
		if stderr != "" || (err == nil) != success || r.OK != success || r.Compiler != "bootstrap" || r.SchemaVersion != 1 || r.LockFile != target || r.Files == nil || r.Diagnostics == nil {
			t.Fatalf("deps: %v %s %s", err, out, stderr)
		}
		return r
	}
	r := dependencyCall(false, true)
	if len(r.Files) != 3 {
		t.Fatalf("closure: %+v", r.Files)
	}
	before, _ := os.ReadFile(target)
	dependencyCall(true, true)
	var seed bytes.Buffer
	if err := depsCommandOutput([]string{"--json", "--check", root}, &seed, io.Discard); err != nil {
		t.Fatal(err)
	}
	var seedReport dependencyReport
	decode(seed.String(), &seedReport)
	if !reflect.DeepEqual(r.Files, seedReport.Files) {
		t.Fatalf("closure mismatch: %+v != %+v", r.Files, seedReport.Files)
	}
	if err := depsCommandOutput([]string{root}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	dependencyCall(true, true)
	// Object ordering and whitespace are not part of the lock identity.
	var parsed map[string]any
	json.Unmarshal(before, &parsed)
	compact, _ := json.Marshal(parsed)
	write(target, strings.ReplaceAll(strings.ReplaceAll(string(compact), "雪", `\u96ea`), "💡", `\ud83d\udca1`))
	compact, _ = os.ReadFile(target)
	dependencyCall(true, true)
	write(sharedFile, "package shared\nfunc Value()int{return 43}")
	r = dependencyCall(true, false)
	if r.Diagnostics[0].Code != compiler.CodeDependency {
		t.Fatal(r)
	}
	after, _ := os.ReadFile(target)
	if string(after) != string(compact) {
		t.Fatal("--check modified lock")
	}
	dependencyCall(false, true)
	dependencyCall(true, true)
	for _, invalid := range []string{`{}`, `{"version":1,"files":null}`, `{"version":2,"files":[]}`, `{"version":1,"version":1,"files":[]}`, `{"version":1,"files":[],"extra":0}`, `{"version":1,"files":[]} {}`} {
		write(target, invalid)
		r = dependencyCall(true, false)
		if r.Diagnostics[0].Code != compiler.CodeSyntax {
			t.Fatalf("malformed lock: %+v", r)
		}
		if err := depsCommandOutput([]string{"--check", root}, io.Discard, io.Discard); err == nil {
			t.Fatal("seed accepted malformed lock", invalid)
		}
	}
	write(target, "keep me")
	write(sharedFile, "package broken\nfunc {")
	dependencyCall(false, false)
	after, _ = os.ReadFile(target)
	if string(after) != "keep me" {
		t.Fatal("failure overwrote lock")
	}
	write(sharedFile, "package shared\nfunc Value()int{return 43}")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sharedFile, target); err != nil {
		t.Fatal(err)
	}
	dependencyCall(false, false)
	dependencyCall(true, false)
	// Selecting one root file must exclude its siblings, and no imports produce [].
	single := filepath.Join(work, "single.lang")
	write(single, "package main\nfunc main(){panic(\"never execute\")}")
	out, stderr, err = invoke("deps", "--json", single)
	decode(out, &r)
	if err != nil || stderr != "" || !r.OK || len(r.Files) != 0 {
		t.Fatalf("single: %v %s %s", err, out, stderr)
	}
	t.Run("symlink_parent_path", func(t *testing.T) {
		project := filepath.Join(work, "symlink-parent")
		for _, dir := range []string{"real/child", "a", "b"} {
			if err := os.MkdirAll(filepath.Join(project, dir), 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink("real/child", filepath.Join(project, "link")); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(project, "main.lang"), "package main\nimport \"./a\"\nfunc main(){println(a.Value())}")
		write(filepath.Join(project, "real", "main.lang"), "package main\nimport \"./b\"\nfunc main(){println(b.Value())}")
		write(filepath.Join(project, "a", "value.lang"), "package a\nfunc Value()int{return 111}")
		dependency := filepath.Join(project, "b", "value.lang")
		write(dependency, "package b\nfunc Value()int{return 222}")
		// Do not use filepath.Join: it would erase the symlink/.. input we test.
		selected := project + "/link/../main.lang"
		out, stderr, err := invoke("run", selected)
		if err != nil || stderr != "" || out != "222\n" {
			t.Fatalf("selected program: %v %s %s", err, out, stderr)
		}
		lockPath := filepath.Join(project, "linglang.lock")
		physicalProject, err := filepath.EvalSymlinks(project)
		if err != nil {
			t.Fatal(err)
		}
		for _, input := range []string{selected, "symlink-parent/link/../main.lang"} {
			expectedLock := lockPath
			if !filepath.IsAbs(input) {
				// OTP resolves relative paths from the physical child cwd. On
				// macOS, the Go test temp path may use its /var symlink alias.
				expectedLock = filepath.Join(physicalProject, "linglang.lock")
			}
			out, stderr, err := invoke("deps", "--json", input)
			var report dependencyReport
			decode(out, &report)
			if err != nil || stderr != "" || !report.OK || report.LockFile != expectedLock || len(report.Files) != 1 || report.Files[0].Path != "b/value.lang" {
				t.Fatalf("selected closure for %s: %v %s %s", input, err, out, stderr)
			}
			if err := depsCommandOutput([]string{"--check", selected}, io.Discard, io.Discard); err != nil {
				t.Fatalf("seed rejected bootstrap lock: %v", err)
			}
		}
		// Verify the reverse direction, then ensure drift checks cover the source
		// actually compiled and leave the reviewed lock untouched on failure.
		if err := depsCommandOutput([]string{selected}, io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}
		out, stderr, err = invoke("deps", "--check", "--json", selected)
		if err != nil || stderr != "" {
			t.Fatalf("bootstrap rejected seed lock: %v %s %s", err, out, stderr)
		}
		before, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		write(dependency, "package b\nfunc Value()int{return 333}")
		out, stderr, err = invoke("deps", "--check", "--json", selected)
		var report dependencyReport
		decode(out, &report)
		if err == nil || stderr != "" || report.OK || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != compiler.CodeDependency {
			t.Fatalf("missed selected dependency change: %v %s %s", err, out, stderr)
		}
		after, err := os.ReadFile(lockPath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("--check changed lock: %v", err)
		}
	})
}
