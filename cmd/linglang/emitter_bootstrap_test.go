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

func TestBootstrapEmitterPrograms(t *testing.T) {
	type fixture struct{ name, code, want, failure string }
	fixtures := []fixture{
		{"operator_chain", `func main(){a:=1;println(a+2+3+4,a+a+a)}`, "10 3\n", ""},
		{"constants_and_shadowing", `const Big=1<<200;const Answer=(Big+7)-Big;func main(){const Answer=8;println(Answer);{const Answer=9;println(Answer)};println(Answer)}`, "8\n9\n8\n", ""},
		{"early_returns", `func f(n int)int{if n<0{return -1}else if n>0{return 1}else{return 0}};func main(){println(f(-7),f(0),f(7))}`, "-1 0 1\n", ""},
		{"parameters", `func sum(a,b int)int{return a+b};func ignore(_ int,_ bool)int{return 3};func unused(int)int{return 4};func main(){println(sum(2,5),ignore(1,true),unused(9))}`, "7 3 4\n", ""},
		{"short_circuit", `func fail()bool{panic("must not run")};func main(){println(false&&fail(),true||fail())}`, "false true\n", ""},
		{"strings", `func main(){s:="雪\x00\xff";println(len(s),byteAt(s,4),len("x")+'a');s+="!";println(len(s),trim(" ok "),slice("abcd",1,3))}`, "5 255 98\n6 ok bc\n", ""},
		{"mutations", `func main(){var n int;var s string;var b bool;n=4;n+=3;n*=2;n--;s="ok";b=n==13;println(n,s,b);_=n+1}`, "13 ok true\n", ""},
		{"bitwise", `func main(){a:=13;b:=6;println(a&b,a|b,a^b,a&^b,^a);a&=b;a|=8;a^=3;a&^=5;a<<=3;a>>=4;println(a);n:=64;a=-1;println(a>>n,a<<n);n=0;x:=1;println(x<<(((1<<63)<<n)>>62),x<<(((1<<n)+18446744073709551615)/2))}`, "4 15 11 9 -14\n5\n-1 0\n4 1\n", ""},
		{"runtime_rune_int_context", `func take(x int)int{return x};func shifted(n int)int{return 'a'<<n};func main(){n:=30;var x int='a'<<n;println(x,take('a'<<n),shifted(n),('a'<<n)+len(""));n=32;x='a'<<n;println(x,('a'<<n)==x);n=0;x=1;println(x<<('a'<<n),'a','a'<<2)}`, "104152956928 104152956928 104152956928 104152956928\n416611827712 true\n0 97 388\n", ""},
		{"contextual_rune", `func main(){n:=0;var x int=('a'+(1<<40))<<n;println(x)}`, "1099511627873\n", ""},
		{"builtin_shadow", `func println(n int){print("local",n)};func main(){println(7)}`, "local7", ""},
		{"negative_shift", `func main(){n:=-1;println(1<<n)}`, "", "linglang_negative_shift"},
		{"panic_cleanup", `func f(n int){x:=n+1;_=x;panic("bean down")};func main(){f(1)}`, "", "bean down"},
		{"assert_location", "func main(){\nassert(false)\n}", "", "fixture.lang:3: assertion failed"},
	}
	files, err := readSources("../../bootstrap/emitter")
	if err != nil {
		t.Fatal(err)
	}
	run := func(dir string, stress bool, args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "erl", append([]string{"-noshell", "-pa", dir, "-eval", evaluationScript(stress, true)}, args...)...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("VM timeout: %s\n%s", stdout.String(), stderr.String())
		}
		return stdout.String(), stderr.String(), err
	}
	for _, baseline := range []bool{false, true} {
		t.Run(fmt.Sprintf("baseline=%v", baseline), func(t *testing.T) {
			program, err := compiler.CompileFilesWithOptions(files, compiler.Options{DisableOptimizations: baseline})
			if err != nil {
				t.Fatal(err)
			}
			emitterDir := t.TempDir()
			if err := build(emitterDir, program); err != nil {
				t.Fatal(err)
			}
			for _, f := range fixtures {
				t.Run(f.name, func(t *testing.T) {
					sourceDir := t.TempDir()
					path := filepath.Join(sourceDir, "fixture.lang")
					code := []byte("package main\n" + f.code)
					if err := os.WriteFile(path, code, 0600); err != nil {
						t.Fatal(err)
					}
					// Compare execution with the seed, including exception cleanup.
					seed, err := compiler.CompileWithOptions(path, code, compiler.Options{DisableOptimizations: baseline})
					if err != nil {
						t.Fatal(err)
					}
					seedDir := t.TempDir()
					if err := build(seedDir, seed); err != nil {
						t.Fatal(err)
					}
					seedOut, seedErr, seedFailure := run(seedDir, true)
					module, stderr, err := run(emitterDir, false, "-extra", path)
					if err != nil {
						t.Fatalf("emission: %v\n%s", err, stderr)
					}
					emitterCleanGC(t, stderr)
					generatedDir := t.TempDir()
					if err := build(generatedDir, module); err != nil {
						t.Fatalf("emitted Erlang: %v\n%s", err, module)
					}
					out, stderr, failure := run(generatedDir, true)
					emitterCleanGC(t, stderr)
					emitterCleanGC(t, seedErr)
					if out != seedOut || out != f.want || (failure == nil) != (seedFailure == nil) {
						t.Fatalf("got %q (%v), seed %q (%v), want %q\n%s", out, failure, seedOut, seedFailure, f.want, stderr)
					}
					if f.failure != "" && (failure == nil || !strings.Contains(stderr, f.failure) || !strings.Contains(seedErr, f.failure)) {
						t.Fatalf("missing failure %q: %v\n%s\nseed: %s", f.failure, failure, stderr, seedErr)
					}
					if f.failure == "" && failure != nil {
						t.Fatalf("runtime: %v\n%s", failure, stderr)
					}
				})
			}
		})
	}
}

func TestBootstrapEmitterPackedWithoutGo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isolated symlink PATH requires Unix")
	}
	files, err := readSources("../../bootstrap/emitter")
	if err != nil {
		t.Fatal(err)
	}
	program, err := compiler.CompileFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	artifact := filepath.Join(dir, "emitter")
	if err := pack(artifact, program, false, false); err != nil {
		t.Fatal(err)
	}
	otp := filepath.Join(dir, "otp")
	if err := os.Mkdir(otp, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"erl", "erlc", "escript", "dirname"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(otp, name)); err != nil {
			t.Fatal(err)
		}
	}
	var env []string
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "PATH=") {
			env = append(env, item)
		}
	}
	env = append(env, "PATH="+otp)
	invoke := func(name string, args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatal("isolated command timed out")
		}
		return stdout.String(), stderr.String(), err
	}
	a, _ := filepath.Abs("../../examples/bootstrap_demo/fibonacci.lang")
	b, _ := filepath.Abs("../../examples/bootstrap_demo/main.lang")
	module, stderr, err := invoke(artifact, b, a)
	if err != nil {
		t.Fatalf("packed emission: %v\n%s", err, stderr)
	}
	forward, stderr, err := invoke(artifact, a, b)
	if err != nil || module != forward {
		t.Fatalf("file order changed output: %v\n%s", err, stderr)
	}
	for name, source := range compiler.RuntimeSources() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "linglang_program.erl"), []byte(module), 0600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = invoke(filepath.Join(otp, "erlc"), "-o", dir, filepath.Join(dir, "linglang_rt.erl"), filepath.Join(dir, "linglang_sup.erl"), filepath.Join(dir, "linglang_program.erl"))
	if err != nil {
		t.Fatalf("OTP compilation: %v\n%s", err, stderr)
	}
	out, stderr, err := invoke(filepath.Join(otp, "erl"), "-noshell", "-pa", dir, "-eval", evaluationScript(true, true))
	want := "compiler has entered the chat\n55 beans. all accounted for.\nthis bean has its own scope\nactual answer: 55\nreceipt: 16\n"
	if err != nil || out != want {
		t.Fatalf("OTP-only program: %q (%v)\n%s", out, err, stderr)
	}
	emitterCleanGC(t, stderr)
	for _, code := range []string{"func main(){for{break}}", "func f()int{};func main(){}", "const N=1<<100;func main(){println(N)}", "func main(){n:=30;println('a'<<n)}", "func main(){n:=30;println('a'>>n)}", "func main(){n:=30;println(('a'<<n)==0)}", "func main(){n:=30;println(('a'<<n)<0)}", "func main(){n:=30;println(^('a'<<n))}", "func main(){n:=30;println(-('a'<<n))}", "func main(){n:=30;println(('a'<<n)+1)}", "func main(){n:=30;_ = 'a'<<n}", "func main(){n:=30;panic('a'<<n)}"} {
		path := filepath.Join(dir, "bad.lang")
		if err := os.WriteFile(path, []byte("package main\n"+code), 0600); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := invoke(artifact, path)
		if err == nil || out != "" || !strings.Contains(stderr, path+":2:") {
			t.Fatalf("missing clean source diagnostic: %q (%v)\n%s", out, err, stderr)
		}
	}
	large := "package main\nconst S0=\"x\";"
	for i := 1; i <= 21; i++ {
		large += fmt.Sprintf("const S%d=S%d+S%d;", i, i-1, i-1)
	}
	large += "func main(){println(S21)}"
	path := filepath.Join(dir, "large.lang")
	if err := os.WriteFile(path, []byte(large), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err = invoke(artifact, path)
	if err == nil || out != "" || !strings.Contains(stderr, "string literal exceeds 1 MiB") {
		t.Fatalf("missing bounded literal diagnostic: %v\n%s", err, stderr)
	}
}

func emitterCleanGC(t *testing.T, stderr string) {
	t.Helper()
	for _, value := range []string{"live_cells => 0", "root_entries => 0", "root_frames => 0"} {
		if !strings.Contains(stderr, value) {
			t.Fatalf("GC did not finish cleanly (%s):\n%s", value, stderr)
		}
	}
}
