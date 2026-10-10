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

func TestReadSources(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{
		"z.lang":   "package main\nfunc main() {}",
		"a.lang":   "package main\nconst Bean = 1",
		"jobs.txt": "this is not source",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nested.lang"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested.lang", "broken.lang"), []byte("bad source"), 0600); err != nil {
		t.Fatal(err)
	}
	sources, err := readSources(dir)
	if err != nil || len(sources) != 2 || filepath.Base(sources[0].Filename) != "a.lang" || filepath.Base(sources[1].Filename) != "z.lang" {
		t.Fatalf("unexpected directory input: %+v (%v)", sources, err)
	}
	single, err := readSources(filepath.Join(dir, "z.lang"))
	if err != nil || len(single) != 1 || !strings.Contains(string(single[0].Source), "func main()") {
		t.Fatalf("explicit file loaded siblings: %+v (%v)", single, err)
	}
	if _, err := readSources(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no .lang source files") {
		t.Fatalf("empty directory accepted: %v", err)
	}
	if _, err := readSources(filepath.Join(dir, "missing.lang")); err == nil || !strings.Contains(err.Error(), "missing.lang") {
		t.Fatalf("missing source path lost: %v", err)
	}
	t.Run("directory_rejects_streams", func(t *testing.T) {
		if _, err := os.Stat("/dev/null"); err != nil {
			t.Skipf("/dev/null unavailable: %v", err)
		}
		stream := filepath.Join(dir, "stream.lang")
		if err := os.Symlink("/dev/null", stream); err != nil {
			t.Fatal(err)
		}
		if _, err := readSources(dir); err == nil || !strings.Contains(err.Error(), stream+": source must be a regular file") {
			t.Fatalf("directory accepted a non-regular source: %v", err)
		}
	})
}

func TestDirectoryCLI(t *testing.T) {
	work := t.TempDir()
	binary := filepath.Join(work, "linglang")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	t.Run("streamed_source", func(t *testing.T) {
		if _, err := os.Stat("/dev/stdin"); err != nil {
			t.Skipf("/dev/stdin unavailable: %v", err)
		}
		for _, baseline := range []bool{false, true} {
			for _, command := range []string{"emit", "check", "run"} {
				args := []string{command}
				if baseline {
					args = append(args, "--no-opt")
				}
				if command == "run" {
					args = append(args, "--gc-stress", "--gc-stats")
				}
				args = append(args, "/dev/stdin")
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				cmd := exec.CommandContext(ctx, binary, args...)
				// A reader becomes a real stdin pipe in the child, rather than a regular file.
				cmd.Stdin = strings.NewReader("package main\nfunc main() { println(\"bean\") }\n")
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				err := cmd.Run()
				cancel()
				if err != nil {
					t.Fatalf("%s streamed source (baseline=%t): %v\n%s\n%s", command, baseline, err, stdout.String(), stderr.String())
				}
				switch command {
				case "emit":
					if !strings.Contains(stdout.String(), "-module(linglang_program).") {
						t.Fatalf("streamed emit did not generate a module: %s", stdout.String())
					}
				case "check":
					if !strings.HasPrefix(stdout.String(), "Checked /dev/stdin (1 source file") {
						t.Fatalf("streamed check did not succeed: %s", stdout.String())
					}
				case "run":
					if stdout.String() != "bean\n" || !strings.Contains(stderr.String(), "live_cells => 0") || !strings.Contains(stderr.String(), "root_frames => 0") {
						t.Fatalf("streamed run did not finish cleanly: %s\n%s", stdout.String(), stderr.String())
					}
				}
			}
		}
	})
	emptyPath := t.TempDir()
	invoke := func(args []string, withoutErlang bool) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = work
		if withoutErlang {
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "PATH=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "PATH="+emptyPath)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}
	source := filepath.Join(work, "雪 beans")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"01_types.lang":   "package main\nconst Seed = 41\ntype Node struct { n int }",
		"20_helpers.lang": "package main\nfunc node() *Node { return &Node{n: Seed} }",
		"main.lang":       "package main\nfunc main() { for _, arg := range args() { println(arg) }; p := node(); println(p.n + 1); writeFile(\"ran.txt\", \"bean\") }",
		"notes.txt":       "this is not a program",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, baseline := range []bool{false, true} {
		flags := []string{}
		if baseline {
			flags = append(flags, "--no-opt")
		}
		stdout, stderr, err := invoke(append(append([]string{"check"}, flags...), source), true)
		if err != nil || stdout != "Checked "+source+" (3 source files)\n" || stderr != "" {
			t.Fatalf("check without Erlang: %v\n%s\n%s", err, stdout, stderr)
		}
		stdout, stderr, err = invoke(append(append([]string{"emit"}, flags...), source), true)
		if err != nil || !strings.Contains(stdout, "-module(linglang_program).") || stderr != "" {
			t.Fatalf("emit without Erlang: %v\n%s\n%s", err, stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(work, "ran.txt")); !os.IsNotExist(err) {
			t.Fatalf("checking or emitting executed main: %v", err)
		}
		if _, err := os.Stat(filepath.Join(work, "_build")); !os.IsNotExist(err) {
			t.Fatalf("checking or emitting created a build directory: %v", err)
		}
		arguments := append(append([]string{"run", "--gc-stress", "--gc-stats"}, flags...), source, "雪 with spaces", "--gc-stress")
		stdout, stderr, err = invoke(arguments, false)
		want := "雪 with spaces\n--gc-stress\n42\n"
		if err != nil || stdout != want || !strings.Contains(stderr, "live_cells => 0") || !strings.Contains(stderr, "root_frames => 0") {
			t.Fatalf("directory run (baseline=%t): got %q (%v)\n%s", baseline, stdout, err, stderr)
		}
		if data, err := os.ReadFile(filepath.Join(work, "ran.txt")); err != nil || string(data) != "bean" {
			t.Fatalf("directory program didn't execute: %q (%v)", data, err)
		}
		if err := os.Remove(filepath.Join(work, "ran.txt")); err != nil {
			t.Fatal(err)
		}
		buildDir := t.TempDir()
		stdout, stderr, err = invoke(append(append([]string{"build", "-o", buildDir}, flags...), source), false)
		if err != nil || !strings.Contains(stdout, "Built BEAM modules in "+buildDir) {
			t.Fatalf("directory build: %v\n%s\n%s", err, stdout, stderr)
		}
		for _, module := range []string{"linglang_program.beam", "linglang_rt.beam", "linglang_sup.beam", "linglang_server.beam", "linglang_io.beam"} {
			if _, err := os.Stat(filepath.Join(buildDir, module)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, args := range [][]string{{"check", source, "extra"}, {"check", filepath.Join(source, "main.lang")}} {
		_, stderr, err := invoke(args, true)
		if err == nil || (args[1] == source && !strings.Contains(stderr, "expects one source")) || (args[1] != source && !strings.Contains(stderr, "undefined: node")) {
			t.Fatalf("invalid input accepted: %v\n%s", err, stderr)
		}
	}
	t.Run("source_diagnostics", func(t *testing.T) {
		for _, tc := range []struct{ source, location, message string }{
			{"package main\nfunc helper() {\n defer println(1)\n}", "helper.lang:3:", "unsupported statement"},
			{"package main\nfunc helper() {\n n := true\n n = 1\n println(n)\n}", "helper.lang:4:", "cannot use"},
			{"package main\nfunc helper( {", "helper.lang:2:", "expected"},
			{"package beans\nfunc helper() {}", "helper.lang:1:", "only package main"},
		} {
			bad := t.TempDir()
			if err := os.WriteFile(filepath.Join(bad, "main.lang"), []byte("package main\nfunc main() { helper() }"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bad, "helper.lang"), []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := invoke([]string{"check", bad}, true)
			if err == nil || stdout != "" || !strings.Contains(stderr, filepath.Join(bad, tc.location)) || !strings.Contains(stderr, tc.message) {
				t.Fatalf("wrong diagnostic: %v\n%s\n%s", err, stdout, stderr)
			}
		}
	})
}
