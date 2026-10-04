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

// After the seed builds the packed emitter, compilation and execution of an
// actual compiler component use only OTP. Both seed backends provide oracles.
func TestBootstrapEmitterLexerWithoutGo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isolated symlink PATH requires Unix")
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	read := func(directory string) []compiler.SourceFile {
		t.Helper()
		sources, err := readSources(filepath.Join(repo, directory))
		if err != nil {
			t.Fatal(err)
		}
		return sources
	}
	emitter, err := compiler.CompileFiles(read("bootstrap/emitter"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	artifact := filepath.Join(dir, "emitter")
	if err := pack(artifact, emitter, false, true); err != nil {
		t.Fatal(err)
	}
	lexerSources := read("bootstrap/lexer")
	var paths []string
	for _, source := range lexerSources {
		paths = append(paths, source.Filename)
	}
	var seeds []string
	for _, baseline := range []bool{false, true} {
		program, err := compiler.CompileFilesWithOptions(lexerSources, compiler.Options{DisableOptimizations: baseline})
		if err != nil {
			t.Fatal(err)
		}
		seed := filepath.Join(dir, fmt.Sprintf("seed-%v", baseline))
		if err := build(seed, program); err != nil {
			t.Fatal(err)
		}
		seeds = append(seeds, seed)
	}
	otp := filepath.Join(dir, "otp")
	env := isolatedOTPEnvironment(t, otp, true)
	invoke := func(name string, args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = env
		cmd.Dir = dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("OTP-only lexer command timed out: %s\n%s", stdout.String(), stderr.String())
		}
		return stdout.String(), stderr.String(), err
	}
	module, stderr, err := invoke(artifact, paths...)
	if err != nil {
		t.Fatalf("OTP-only lexer emission: %v\n%s", err, stderr)
	}
	emitterCleanGC(t, stderr)
	generated := filepath.Join(dir, "generated")
	if err := os.MkdirAll(generated, 0700); err != nil {
		t.Fatal(err)
	}
	modules := compiler.RuntimeSources()
	modules["linglang_program.erl"] = module
	args := []string{"-o", generated}
	for name, source := range modules {
		path := filepath.Join(generated, name)
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		args = append(args, path)
	}
	if out, stderr, err := invoke(filepath.Join(otp, "erlc"), args...); err != nil {
		t.Fatalf("OTP-only lexer compilation: %v\n%s\n%s", err, out, stderr)
	}
	// Print just the language diagnostic in addition to the runner's cleanup
	// stats. Erlang stack locations differ across emitted modules.
	entry := `(try linglang_program:main() catch error:{linglang_panic, LexerReason} -> io:format(standard_error, "lexer diagnostic: ~ts~n", [LexerReason]), erlang:error(lexer_failed) end)`
	diagnostic := func(stderr string) string {
		_, after, _ := strings.Cut(stderr, "lexer diagnostic: ")
		line, _, _ := strings.Cut(after, "\n")
		return line
	}
	run := func(dir, path string, stress bool) (string, string, error) {
		return invoke(filepath.Join(otp, "erl"), "-noshell", "-pa", dir, "-eval", evaluationScriptFor(entry, stress, true), "-extra", path)
	}
	type fixture struct {
		path   string
		stress bool
		fails  bool
	}
	var fixtures []fixture
	for _, path := range append(paths, filepath.Join(repo, "bootstrap/lexer/lexer_test.lang"), filepath.Join(repo, "bootstrap/emitter/emit_switches.lang")) {
		fixtures = append(fixtures, fixture{path: path})
	}
	for i, source := range []string{
		"", "x\n", "\ufeff雪٢ := 0x_FF\r\n雪٢++", "x /* a\nb */ y // end\n",
		"`a\r\nb` '\\U0001F600' \"bean\\n\"",
		"<<= >>= &^= ... ++ -- := <- && || &^ <= != ~",
		"0 0_7 0o_7 0B_1 0X_FF 12_345 99999999999999999999999",
		"0x", "1__2", "\"unterminated", "/* unterminated", "'ab'", "\"\\uD800\"", "@", "// \xff", "`\x00`", "x\n  @",
	} {
		path := filepath.Join(dir, fmt.Sprintf("fixture-%d.lang", i))
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, fixture{path: path, stress: true, fails: i >= 7})
	}
	fixtures = append(fixtures, fixture{path: filepath.Join(dir, "missing.lang"), stress: true, fails: true})
	for _, f := range fixtures {
		t.Run(filepath.Base(f.path), func(t *testing.T) {
			out, stderr, err := run(generated, f.path, f.stress)
			if (err != nil) != f.fails {
				t.Fatalf("lexer exit: %v\n%s", err, stderr)
			}
			emitterCleanGC(t, stderr)
			if f.fails && diagnostic(stderr) == "" {
				t.Fatalf("missing source diagnostic: %s", stderr)
			}
			for _, seed := range seeds {
				seedOut, seedErr, seedFailure := run(seed, f.path, f.stress)
				emitterCleanGC(t, seedErr)
				if out != seedOut || diagnostic(stderr) != diagnostic(seedErr) || (err != nil) != (seedFailure != nil) {
					t.Fatalf("lexer differs from %s: %v / %v\ngot %q\nwant %q\n%s\n%s", filepath.Base(seed), err, seedFailure, out, seedOut, stderr, seedErr)
				}
			}
		})
	}
}
