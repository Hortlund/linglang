package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

const bootstrapPackingScript = `[SourcePath, Output | Inputs] = init:get_plain_arguments(), {ok, Source} = file:read_file(SourcePath), case linglang_rt:build_program(Source, unicode:characters_to_binary(Output), [unicode:characters_to_binary(P) || P <- [SourcePath | Inputs]], false, true) of #{field_6f6b := true} -> halt(0); Result -> io:format(standard_error, "compiler packaging failed: ~tp~n", [Result]), halt(1) end.`

func TestBootstrapCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isolated OTP launcher requires Unix")
	}
	sources, err := readSources("../../bootstrap/emitter")
	if err != nil {
		t.Fatal(err)
	}
	for _, baseline := range []bool{false, true} {
		name := "optimized"
		if baseline {
			name = "cells"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			program, err := compiler.CompileFilesWithOptions(sources, compiler.Options{DisableOptimizations: baseline})
			if err != nil {
				t.Fatal(err)
			}
			cli := filepath.Join(dir, "compiler")
			seedDir := filepath.Join(dir, "seed")
			if err := build(seedDir, program); err != nil {
				t.Fatal(err)
			}
			otp := filepath.Join(dir, "otp")
			env := isolatedOTPEnvironment(t, otp, false)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			packing := exec.CommandContext(ctx, filepath.Join(otp, "erl"), "+S", "1", "-noshell", "-pa", seedDir, "-eval", bootstrapPackingScript, "-extra", filepath.Join(seedDir, "linglang_program.erl"), cli)
			packing.Env = env
			packingOutput, packingErr := packing.CombinedOutput()
			cancel()
			if packingErr != nil {
				t.Fatalf("package seed compiler: %v\n%s", packingErr, packingOutput)
			}
			t.Run("stdin", func(t *testing.T) {
				testBootstrapStdin(t, cli, env)
			})
			t.Run("concurrent_builds", func(t *testing.T) {
				testBootstrapConcurrentBuilds(t, cli, env)
			})
			t.Run("run_lifetime", func(t *testing.T) {
				testBootstrapRunLifetime(t, cli, env)
			})
			invoke := func(executable string, args ...string) (string, string, error) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				cmd := exec.CommandContext(ctx, executable, args...)
				cmd.Env, cmd.Dir = env, dir
				var out, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &stderr
				err := cmd.Run()
				if ctx.Err() != nil {
					t.Fatalf("CLI timeout: %s", stderr.String())
				}
				return out.String(), stderr.String(), err
			}
			pkg := filepath.Join(dir, "雪 package's")
			if err := os.Mkdir(pkg, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(pkg, "main.lang")
			code := []byte(`package main
type N struct{x int}
func node()*N{n:=N{x:7};return &n}
func main(){p:=node();for i:=0;i<30;i++{_=node()};p.x++;println(p.x,join(args(),"|"))}`)
			if err := os.WriteFile(path, code, 0600); err != nil {
				t.Fatal(err)
			}
			// Directory discovery excludes tests and nested packages.
			if err := os.WriteFile(filepath.Join(pkg, "bad_test.lang"), []byte("invalid"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(pkg, "nested.lang"), 0700); err != nil {
				t.Fatal(err)
			}
			out, stderr, err := invoke(cli, "check", pkg)
			if err != nil || out != "" {
				t.Fatalf("check: %q %v\n%s", out, err, stderr)
			}
			emitterCleanGC(t, stderr)
			// A single large write crosses native-port chunk boundaries inside UTF-8.
			stream := filepath.Join(dir, "stream.lang")
			text := strings.Repeat("雪", 30000)
			if err := os.WriteFile(stream, []byte("package main\nfunc main(){println(\""+text+"\")}"), 0600); err != nil {
				t.Fatal(err)
			}
			out, stderr, err = invoke(cli, "run", stream)
			if err != nil || out != text+"\n" {
				t.Fatalf("Unicode stream: %d bytes, %v\n%.1000s", len(out), err, stderr)
			}
			module, stderr, err := invoke(cli, "emit", pkg)
			if err != nil || !strings.Contains(module, "-module(linglang_program).") {
				t.Fatalf("emit: %v\n%s", err, stderr)
			}
			legacy, _, err := invoke(cli, path)
			if err != nil || legacy != module {
				t.Fatal("legacy emission differs from directory emit")
			}
			cellModule, stderr, err := invoke(cli, "emit", "--no-opt", pkg)
			if err != nil || cellModule == module {
				t.Fatalf("cell oracle emission: %v\n%s", err, stderr)
			}
			out, stderr, err = invoke(cli, "run", "--no-opt", "--gc-stress", "--gc-stats", pkg, "--", "--no-opt")
			if err != nil || out != "8 --no-opt\n" {
				t.Fatalf("cell oracle run or argument forwarding: %q %v\n%s", out, err, stderr)
			}
			emitterCleanGC(t, stderr)
			artifact := filepath.Join(dir, "out", "雪 bean's executable")
			out, stderr, err = invoke(cli, "build", "--gc-stress", "--gc-stats", "-o", artifact, pkg)
			if err != nil || !strings.Contains(out, "Built executable in ") {
				t.Fatalf("build: %v\n%s\n%s", err, out, stderr)
			}
			emitterCleanGC(t, stderr)
			// A moved executable has no source checkout, Go, or erlc on PATH.
			data, err := os.ReadFile(artifact)
			if err != nil {
				t.Fatal(err)
			}
			moved := filepath.Join(t.TempDir(), "renamed")
			if err := os.WriteFile(moved, data, 0755); err != nil {
				t.Fatal(err)
			}
			arguments := []string{"snow 雪", "quoted 'value'", ""}
			want := "8 snow 雪|quoted 'value'|\n"
			out, stderr, err = invoke(moved, arguments...)
			if err != nil || out != want {
				t.Fatalf("built execution: %q %v\n%s", out, err, stderr)
			}
			emitterCleanGC(t, stderr)
			out, stderr, err = invoke(cli, append([]string{"run", "--gc-stress", "--gc-stats", pkg, "--"}, arguments...)...)
			if err != nil || out != want {
				t.Fatalf("run: %q %v\n%s", out, err, stderr)
			}
			emitterCleanGC(t, stderr)
			for _, target := range []string{path, filepath.Join(dir, "alias.lang"), filepath.Join(dir, "hardlink.lang")} {
				if target == filepath.Join(dir, "alias.lang") {
					if err := os.Symlink(path, target); err != nil {
						t.Fatal(err)
					}
				} else if target == filepath.Join(dir, "hardlink.lang") {
					if err := os.Link(path, target); err != nil {
						t.Fatal(err)
					}
				}
				out, stderr, err := invoke(cli, "build", "-o", target, pkg)
				if err == nil || out != "" || !strings.Contains(stderr, "refusing to overwrite source") {
					t.Fatalf("source overwritten: %q %v\n%s", out, err, stderr)
				}
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, code) {
					t.Fatal("source changed")
				}
			}
			bad := filepath.Join(dir, "bad.lang")
			for _, command := range []string{"check", "emit", "build", "run"} {
				if err := os.WriteFile(bad, []byte("package main\nfunc main(){unknown()}"), 0600); err != nil {
					t.Fatal(err)
				}
				out, stderr, err := invoke(cli, command, bad)
				if err == nil || out != "" || !strings.Contains(stderr, "unknown") {
					t.Fatalf("bad source accepted by %s: %v\n%s", command, err, stderr)
				}
				emitterCleanGC(t, stderr)
			}
			if err := os.WriteFile(bad, []byte("package main\nfunc missing()int{};func main(){}"), 0600); err != nil {
				t.Fatal(err)
			}
			out, stderr, err = invoke(cli, "check", bad)
			if err == nil || out != "" || !strings.Contains(stderr, "missing return") {
				t.Fatalf("check accepted missing return: %v\n%s", err, stderr)
			}
			if err := os.WriteFile(bad, []byte(`package main
func main(){panic("expected failure")}`), 0600); err != nil {
				t.Fatal(err)
			}
			out, stderr, err = invoke(cli, "run", "--gc-stress", "--gc-stats", bad)
			if err == nil || out != "" || !strings.Contains(stderr, "expected failure") || !strings.Contains(stderr, "program exited with status 1") {
				t.Fatalf("runtime failure lost: %q %v\n%s", out, err, stderr)
			}
			emitterCleanGC(t, stderr)
			for _, args := range [][]string{{}, {"build", "-o"}, {"run", "--wat", pkg}, {"emit", "--", pkg}} {
				out, _, err := invoke(cli, args...)
				if err == nil || out != "" {
					t.Fatalf("invalid arguments accepted: %v", args)
				}
			}
		})
	}
}

func testBootstrapStdin(t *testing.T, cli string, env []string) {
	t.Helper()
	// Also exercise Unicode temporary executable paths at the OS argv boundary.
	dir := filepath.Join(t.TempDir(), "雪 å's temporary directory")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	env = append(append([]string{}, env...), "TMPDIR="+dir)
	source := filepath.Join(dir, "stdin.lang")
	// Pipes can return a partial read; keep reading until the inherited EOF.
	code := `package main
func main(){var data string;for{input:=readFile("/dev/stdin");assert(input.ok);if len(input.value)==0{break};data+=input.value};saved:=writeFile(head(args()).value,data);assert(saved.ok);println(len(data))}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"unicode", []byte(strings.Repeat("snow 雪\n", 20000))},
		{"binary", bytes.Repeat([]byte{0, 255, 128, '\n'}, 20000)},
	} {
		t.Run(input.name, func(t *testing.T) {
			output := filepath.Join(dir, input.name+".out")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, cli, "run", "--gc-stress", "--gc-stats", source, "--", output)
			cmd.WaitDelay = time.Second
			cmd.Env = env
			// A reader supplies a real pipe, including an EOF after the final byte.
			cmd.Stdin = bytes.NewReader(input.data)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("stdin run: %v\n%s", err, stderr.String())
			}
			if stdout.String() != fmt.Sprintf("%d\n", len(input.data)) {
				t.Fatalf("stdin length: %q", stdout.String())
			}
			got, err := os.ReadFile(output)
			if err != nil || !bytes.Equal(got, input.data) {
				t.Fatalf("stdin bytes changed: %d bytes, %v", len(got), err)
			}
			emitterCleanGC(t, stderr.String())
		})
	}
	// Source streamed into the compiler is consumed before the child inherits EOF.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, "run", "/dev/stdin")
	cmd.Env = env
	cmd.Stdin = strings.NewReader("package main\nfunc main(){println(\"streamed source\")}")
	out, err := cmd.CombinedOutput()
	if err != nil || !bytes.HasPrefix(out, []byte("streamed source\n")) {
		t.Fatalf("streamed source run: %v\n%s", err, out)
	}
}

func testBootstrapConcurrentBuilds(t *testing.T, cli string, env []string) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "parallel.lang")
	if err := os.WriteFile(source, []byte("package main\nfunc main(){println(\"parallel build\")}"), 0600); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(dir, "outputs")
	if err := os.Mkdir(outputDir, 0700); err != nil {
		t.Fatal(err)
	}
	const builds = 24
	type result struct {
		path string
		out  []byte
		err  error
	}
	results := make(chan result, builds)
	slots := make(chan struct{}, 12)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i := 0; i < builds; i++ {
		path := filepath.Join(outputDir, fmt.Sprintf("program-%d", i))
		go func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			cmd := exec.CommandContext(ctx, cli, "build", "-o", path, source)
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			results <- result{path, out, err}
		}()
	}
	var artifacts []string
	for i := 0; i < builds; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("concurrent build %s: %v\n%s", r.path, r.err, r.out)
		} else {
			artifacts = append(artifacts, r.path)
		}
	}
	for _, path := range artifacts {
		cmd := exec.CommandContext(ctx, path)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil || string(out) != "parallel build\n" {
			t.Errorf("concurrently built artifact: %v\n%s", err, out)
		}
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil || len(entries) != builds {
		t.Errorf("output directory has temporary files or missing executables: %d entries, %v", len(entries), err)
	}
}
