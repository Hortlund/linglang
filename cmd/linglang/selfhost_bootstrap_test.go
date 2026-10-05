package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

// A full rebuild still takes substantially longer than the fixture suites,
// so keep the proof opt-in rather than adding it to lightweight push checks.
func TestBootstrapSelfHostProof(t *testing.T) {
	if os.Getenv("LINGLANG_SELFHOST") != "1" {
		t.Skip("set LINGLANG_SELFHOST=1 to run the Go -> A -> B -> C proof")
	}
	if runtime.GOOS == "windows" {
		t.Skip("isolated OTP launchers require Unix")
	}
	root, err := filepath.Abs("../../bootstrap/emitter")
	if err != nil {
		t.Fatal(err)
	}
	sources, err := readSources(root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, source := range sources {
		paths = append(paths, source.Filename)
	}
	a, err := compiler.CompileFiles(sources)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	aDir := filepath.Join(dir, "a")
	if err := build(aDir, a); err != nil {
		t.Fatal(err)
	}
	otp := filepath.Join(dir, "otp")
	env := isolatedOTPEnvironment(t, otp, true)
	// Expected OTP restart reports contain timestamps and PIDs. Silence them
	// only in this deterministic output oracle, including its packed children.
	env = append(env, "ERL_FLAGS=-kernel logger_level emergency")
	invoke := func(name string, args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env, cmd.Dir = env, dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("self-host command timed out: %s", stderr.String())
		}
		return stdout.String(), stderr.String(), err
	}
	entry := `(try linglang_program:main() catch error:{linglang_panic, BootstrapReason} -> io:format(standard_error, "bootstrap diagnostic: ~ts~n", [BootstrapReason]), erlang:error(bootstrap_failed) end)`
	run := func(directory string, stress bool, paths ...string) (string, string, error) {
		return invoke(filepath.Join(otp, "erl"), append([]string{"+S", "1", "-noshell", "-pa", directory, "-eval", evaluationScriptFor(entry, stress, true), "-extra"}, paths...)...)
	}
	compile := func(name, module string) string {
		t.Helper()
		directory := filepath.Join(dir, name)
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		files := compiler.RuntimeSources()
		files["linglang_program.erl"] = module
		args := []string{"-o", directory}
		for filename, source := range files {
			path := filepath.Join(directory, filename)
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			args = append(args, path)
		}
		if out, stderr, err := invoke(filepath.Join(otp, "erlc"), args...); err != nil {
			t.Fatalf("compile %s: %v\n%s\n%s", name, err, out, stderr)
		}
		return directory
	}
	started := time.Now()
	b, stderr, err := run(aDir, false, paths...)
	if err != nil {
		t.Fatalf("A -> B: %v\n%s", err, stderr)
	}
	emitterCleanGC(t, stderr)
	t.Logf("A -> B: %s", time.Since(started))
	bDir := compile("b", b)
	t.Log("A built B; rebuilding through B")
	started = time.Now()
	c, stderr, err := run(bDir, false, paths...)
	if err != nil {
		t.Fatalf("B -> C: %v\n%s", err, stderr)
	}
	emitterCleanGC(t, stderr)
	if b != c {
		t.Fatalf("B/C compiler output differs: %x / %x", sha256.Sum256([]byte(b)), sha256.Sum256([]byte(c)))
	}
	t.Logf("B -> C: %s", time.Since(started))
	cDir := compile("c", c)
	t.Logf("B/C exact output: %d bytes, SHA-256 %x", len(c), sha256.Sum256([]byte(c)))
	// Package the self-built C source through OTP itself. An optional destination
	// preserves the actual compiler executable after the temporary proof ends.
	artifact := os.Getenv("LINGLANG_SELFHOST_OUT")
	if artifact == "" {
		artifact = filepath.Join(dir, "compiler-c")
	} else {
		artifact, err = filepath.Abs(artifact)
		if err != nil {
			t.Fatal(err)
		}
	}
	packing := strings.Replace(bootstrapPackingScript, "false, true)", "false, false)", 1)
	packingArgs := []string{"+S", "1", "-noshell", "-pa", cDir, "-eval", packing, "-extra", filepath.Join(cDir, "linglang_program.erl"), artifact}
	if out, stderr, err := invoke(filepath.Join(otp, "erl"), append(packingArgs, paths...)...); err != nil {
		t.Fatalf("package C: %v\n%s\n%s", err, out, stderr)
	}
	t.Logf("Self-built C executable: %s", artifact)
	t.Run("cli_stdin", func(t *testing.T) {
		testBootstrapStdin(t, artifact, env)
	})
	t.Run("cli_concurrent_builds", func(t *testing.T) {
		testBootstrapConcurrentBuilds(t, artifact, env)
	})
	t.Run("cli_run_lifetime", func(t *testing.T) {
		testBootstrapRunLifetime(t, artifact, env)
	})
	for _, f := range []struct{ name, code, want string }{
		{"otp_timers", otpTimerFixture, otpTimerOutput},
		{"otp_messages", otpMessagesFixture, "true 7 true 8\nfalse 0 true true\ntrue true true\n"},
		{"otp_supervisor", otpSupervisorFixture, "true true true\ntrue true true\ntrue true true\n"},
		{"otp_restart", otpRestartFixture, "true true true\ntrue true\n"},
		{"native_range", nativeRangeFixture, "4\n7\n100\n100\n0 10\n1 20\n2 30\n"},
		{"readonly_scopes", readonlyScopesFixture, "1 0 ok true\n2\n3\n1\n4\n1\n8\n0 10\n2 32\n1\n"},
		{"readonly_iteration_escape", readonlyEscapeFixture, "10 11\n20 30\ntrue true\n"},
		{"native_locals", `func f(n int,s string,b bool)int{n++;x:=n;n+=x;x*=2;s+="!";b=!b;println(n,x,s,b);return x};func main(){println(f(3,"ok",true))}`, "8 8 ok! false\n8\n"},
		{"range_parameter_assignment", `func f(i,n int)int{for i,n=range (List[int]{4,7}){println(i,n)};return i+n};func main(){println(f(10,20))}`, "0 4\n1 7\n8\n"},
		{"control_and_collections", `func main(){total:=0;for _,n:=range (List[int]{1,2,3}){switch n{case 2:continue;default:m:=Map[int,int]{n:n*2};total+=get(m,n).value;break}};println(total)}`, "8\n"},
		{"escaped_pointer", `type N struct{x int};func node()*N{n:=N{x:7};return &n};func main(){p:=node();for i:=0;i<30;i++{_=node()};switch p.x{case 7:p.x++;default:panic("wrong")};println(p.x)}`, "8\n"},
		{"return_and_rune", `func value(n int)int{switch n{case 1:return runeAt("雪",0).value;default:return 0}};func main(){println(value(1),runeAt("\xff",0).ok)}`, "38634 false\n"},
	} {
		t.Run(f.name, func(t *testing.T) {
			path := filepath.Join(dir, f.name+".lang")
			code := []byte("package main\n" + f.code)
			if err := os.WriteFile(path, code, 0600); err != nil {
				t.Fatal(err)
			}
			if out, stderr, err := invoke(artifact, "check", path); err != nil || out != "" {
				t.Fatalf("C check: %v\n%s\n%s", err, out, stderr)
			}
			module, stderr, err := run(cDir, false, path)
			if err != nil {
				t.Fatalf("C emission: %v\n%s", err, stderr)
			}
			emitterCleanGC(t, stderr)
			// Exercise the usable self-built driver as well as raw module emission.
			packed := filepath.Join(dir, f.name+"-packed")
			if out, stderr, err := invoke(artifact, "build", "--gc-stress", "--gc-stats", "-o", packed, path); err != nil || !strings.Contains(out, "Built executable in ") {
				t.Fatalf("C build: %v\n%s\n%s", err, out, stderr)
			}
			out, stderr, err := invoke(packed)
			if err != nil || out != f.want {
				t.Fatalf("C packed execution: %q %v\n%s", out, err, stderr)
			}
			emitterCleanGC(t, stderr)
			out, stderr, err = invoke(artifact, "run", "--gc-stress", "--gc-stats", path)
			if err != nil || out != f.want {
				t.Fatalf("C run: %q %v\n%s", out, err, stderr)
			}
			emitterCleanGC(t, stderr)
			out, stderr, err = invoke(artifact, "run", "--no-opt", "--gc-stress", "--gc-stats", path)
			if err != nil || out != f.want {
				t.Fatalf("C cell oracle run: %q %v\n%s", out, err, stderr)
			}
			emitterCleanGC(t, stderr)
			generated := compile(f.name, module)
			out, stderr, err = run(generated, true)
			if err != nil || out != f.want {
				t.Fatalf("C execution: %q (%v), want %q\n%s", out, err, f.want, stderr)
			}
			emitterCleanGC(t, stderr)
			seed, err := compiler.Compile(path, code)
			if err != nil {
				t.Fatal(err)
			}
			seedOut, seedErr, seedFailure := run(compile(f.name+"-seed", seed), true)
			emitterCleanGC(t, seedErr)
			if seedFailure != nil || seedOut != out {
				t.Fatalf("C differs from seed: %q (%v)\n%s", seedOut, seedFailure, seedErr)
			}
		})
	}
	for _, code := range []string{"func f()int{};func main(){}", "func main(){unknown()}", "func main(){switch {"} {
		path := filepath.Join(dir, "bad.lang")
		if err := os.WriteFile(path, []byte("package main\n"+code), 0600); err != nil {
			t.Fatal(err)
		}
		diagnostic := func(directory string) string {
			out, stderr, err := run(directory, false, path)
			if err == nil || out != "" {
				t.Fatalf("bad source accepted: %v\n%s", err, out)
			}
			emitterCleanGC(t, stderr)
			_, after, _ := strings.Cut(stderr, "bootstrap diagnostic: ")
			line, _, _ := strings.Cut(after, "\n")
			if line == "" {
				t.Fatalf("missing diagnostic: %s", stderr)
			}
			return line
		}
		if got, want := diagnostic(cDir), diagnostic(aDir); got != want {
			t.Fatalf("C/A diagnostics differ: %q / %q", got, want)
		}
	}
}
