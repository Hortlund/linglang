package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPackCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isolated PATH and direct shebang execution require Unix")
	}
	buildDir := t.TempDir()
	binary := filepath.Join(buildDir, "linglang")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	// Launch from an isolated folder with only OTP and its shell helper on PATH.
	// No Go, linglang, erlc, or source checkout is available to the executable.
	runtimeDir := t.TempDir()
	for _, name := range []string{"escript", "erl", "dirname"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(runtimeDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	var environment []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PATH=") && !strings.HasPrefix(entry, "ERL_LIBS=") && !strings.HasPrefix(entry, "ERL_FLAGS=") && !strings.HasPrefix(entry, "ERL_AFLAGS=") && !strings.HasPrefix(entry, "ERL_ZFLAGS=") {
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "PATH="+runtimeDir)
	invoke := func(dir string, env []string, command string, args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Dir, cmd.Env = dir, env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}
	packProgram := func(t *testing.T, source, dir string, baseline bool) string {
		t.Helper()
		artifact := filepath.Join(dir, "out", "program.escript")
		args := []string{"pack", "--gc-stress", "--gc-stats", "-o", artifact}
		if baseline {
			args = append(args, "--no-opt")
		}
		args = append(args, source)
		out, stderr, err := invoke(dir, nil, binary, args...)
		if err != nil || !strings.Contains(out, "Packed executable in "+artifact) {
			t.Fatalf("pack: %v\n%s\n%s", err, out, stderr)
		}
		data, err := os.ReadFile(artifact)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"linglang_cli.beam", "linglang_program.beam", "linglang_rt.beam", "linglang_sup.beam"}
		if len(reader.File) != len(want) {
			t.Fatalf("unexpected archive contents: %+v", reader.File)
		}
		for i, file := range reader.File {
			if file.Name != want[i] {
				t.Fatalf("unexpected archive entry: %s", file.Name)
			}
			r, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			beam, err := io.ReadAll(r)
			r.Close()
			if err != nil || !bytes.HasPrefix(beam, []byte("FOR1")) {
				t.Fatalf("invalid BEAM entry %s: %v", file.Name, err)
			}
		}
		info, err := os.Stat(artifact)
		if err != nil || info.Mode().Perm()&0111 == 0 {
			t.Fatalf("artifact is not executable: %v", err)
		}
		// Moving and renaming prove the launcher doesn't depend on the output name.
		clean := t.TempDir()
		moved := filepath.Join(clean, "雪 bean \"renamed\"")
		if err := os.WriteFile(moved, data, 0755); err != nil {
			t.Fatal(err)
		}
		return moved
	}
	runArtifact := func(path string, args ...string) (string, string, error) {
		return invoke(filepath.Dir(path), environment, path, args...)
	}
	cleanGC := func(t *testing.T, stderr string) {
		t.Helper()
		for _, message := range []string{"live_cells => 0", "root_frames => 0"} {
			if !strings.Contains(stderr, message) {
				t.Fatalf("missing %q:\n%s", message, stderr)
			}
		}
	}
	for _, baseline := range []bool{false, true} {
		name := "optimized"
		if baseline {
			name = "baseline"
		}
		t.Run(name, func(t *testing.T) {
			t.Run("arguments_and_restarts", func(t *testing.T) {
				dir := t.TempDir()
				source := filepath.Join(dir, "arguments.lang")
				code := `package main
type Ready struct { pid Pid; values List[string] }
func bracketArgs(values List[string]) string {
 text := ""
 for _, value := range values { text += "[" + value + "]" }
 return text
}
func worker(parent Pid) { send(parent, Ready{pid: self(), values: args()}); receive[int](-1) }
func main() {
 println("args:", len(args()))
 for _, value := range args() { println(value) }
 s := startSupervisor(3, 5)
 child := supervise(s, "argv", worker, self(), ChildOptions{})
 if !child.ok { panic(child.reason) }
 first := receive[Ready](5000)
 if !first.ok || bracketArgs(first.value.values) != bracketArgs(args()) { panic("worker argv mismatch") }
 if !stopChild(s, "argv") { panic("child did not stop") }
 replacement := restartChild(s, "argv")
 if !replacement.ok { panic(replacement.reason) }
 second := receive[Ready](5000)
 if !second.ok || bracketArgs(second.value.values) != bracketArgs(args()) { panic("replacement argv mismatch") }
 println("restarted:", first.value.pid != second.value.pid)
 println("stopped:", stopSupervisor(s))
}`
				if err := os.WriteFile(source, []byte(code), 0600); err != nil {
					t.Fatal(err)
				}
				artifact := packProgram(t, source, dir, baseline)
				if err := os.RemoveAll(dir); err != nil {
					t.Fatal(err)
				}
				for _, args := range [][]string{nil, {"雪 with spaces", "--gc-stress", "-extra", "\"quotes\";$(literal)", ""}} {
					out, stderr, err := runArtifact(artifact, args...)
					want := "args: 0\nrestarted: true\nstopped: true\n"
					if len(args) != 0 {
						want = "args: 5\n" + strings.Join(args, "\n") + "\nrestarted: true\nstopped: true\n"
					}
					if err != nil || out != want {
						t.Fatalf("isolated argv: %v\ngot %q, want %q\n%s", err, out, want, stderr)
					}
					cleanGC(t, stderr)
				}
			})
			t.Run("prime_lab", func(t *testing.T) {
				source, err := filepath.Abs("../../examples/prime_lab")
				if err != nil {
					t.Fatal(err)
				}
				artifact := packProgram(t, source, t.TempDir(), baseline)
				dir := filepath.Dir(artifact)
				jobs, err := os.ReadFile("../../examples/prime_jobs.txt")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "雪 jobs.txt"), jobs, 0600); err != nil {
					t.Fatal(err)
				}
				out, stderr, err := runArtifact(artifact, "雪 jobs.txt", "雪 report.csv")
				if err != nil {
					t.Fatalf("isolated prime lab: %v\n%s\n%s", err, out, stderr)
				}
				for _, message := range []string{"Completed: 3 | Worker restarts: 1", "Pool workers: 3 | Peak active: 3", "Supervisor stopped: true"} {
					if !strings.Contains(out, message) {
						t.Fatalf("missing %q:\n%s", message, out)
					}
				}
				cleanGC(t, stderr)
				report, err := os.ReadFile(filepath.Join(dir, "雪 report.csv"))
				want := "job,limit,prime_count,largest_prime\n1,100,25,97\n2,1000,168,997\n3,10000,1229,9973\n"
				if err != nil || string(report) != want {
					t.Fatalf("report: %q (%v), want %q", report, err, want)
				}
			})
			t.Run("failure_exits", func(t *testing.T) {
				for _, tc := range []struct {
					source, message string
					stats           bool
				}{
					{`package main
func main() { var p *int; println(*p) }`, "linglang_nil_pointer", true},
					{`package main
func worker(n int) { panic("restart limit") }
func main() { s := startSupervisor(0, 5); supervise(s, "crash", worker, 0, ChildOptions{}); receive[int](-1) }`, "linglang process exited: shutdown", false},
				} {
					dir := t.TempDir()
					source := filepath.Join(dir, "fail.lang")
					if err := os.WriteFile(source, []byte(tc.source), 0600); err != nil {
						t.Fatal(err)
					}
					artifact := packProgram(t, source, dir, baseline)
					out, stderr, err := runArtifact(artifact)
					exit, ok := err.(*exec.ExitError)
					if !ok || exit.ExitCode() != 1 || !strings.Contains(stderr, tc.message) {
						t.Fatalf("failure exit: %v\n%s\n%s", err, out, stderr)
					}
					if tc.stats {
						cleanGC(t, stderr)
					} else if !strings.Contains(stderr, "linglang GC: unavailable") {
						t.Fatal(stderr)
					}
					if _, err := os.Stat(filepath.Join(filepath.Dir(artifact), "erl_crash.dump")); !os.IsNotExist(err) {
						t.Fatalf("unexpected crash dump: %v", err)
					}
				}
			})
		})
	}
	t.Run("output_and_failures", func(t *testing.T) {
		dir := t.TempDir()
		source := filepath.Join(dir, "bean.lang")
		valid := []byte("package main\nfunc main() { println(1) }")
		if err := os.WriteFile(source, valid, 0600); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := invoke(dir, nil, binary, "pack", source)
		if err != nil || !strings.Contains(out, filepath.Join(dir, "bean.escript")) {
			t.Fatalf("default output: %v\n%s\n%s", err, out, stderr)
		}
		artifact := filepath.Join(dir, "bean.escript")
		before, err := os.ReadFile(artifact)
		if err != nil {
			t.Fatal(err)
		}
		// Refuse direct, symlink, and hard-link aliases of an input source.
		for _, name := range []string{"direct", "symlink", "hardlink"} {
			output := source
			if name != "direct" {
				output = filepath.Join(dir, name)
				if name == "symlink" {
					err = os.Symlink(source, output)
				} else {
					err = os.Link(source, output)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			_, stderr, err := invoke(dir, nil, binary, "pack", "-o", output, source)
			if err == nil || !strings.Contains(stderr, "refusing to overwrite source") {
				t.Fatalf("source overwrite accepted: %v\n%s", err, stderr)
			}
			if data, err := os.ReadFile(source); err != nil || !bytes.Equal(data, valid) {
				t.Fatalf("source changed: %q (%v)", data, err)
			}
		}
		if err := os.WriteFile(source, []byte("bad source"), 0600); err != nil {
			t.Fatal(err)
		}
		_, _, err = invoke(dir, nil, binary, "pack", "-o", artifact, source)
		if err == nil {
			t.Fatal("bad source packed")
		}
		if data, err := os.ReadFile(artifact); err != nil || !bytes.Equal(data, before) {
			t.Fatal("failed pack replaced existing artifact")
		}
		if err := os.WriteFile(source, valid, 0600); err != nil {
			t.Fatal(err)
		}
		_, stderr, err = invoke(dir, environment, binary, "pack", "-o", artifact, source)
		if err == nil || !strings.Contains(stderr, "erlc is not on PATH") {
			t.Fatalf("missing compiler accepted: %v\n%s", err, stderr)
		}
		if data, err := os.ReadFile(artifact); err != nil || !bytes.Equal(data, before) {
			t.Fatal("missing OTP compiler replaced existing artifact")
		}
		_, stderr, err = invoke(dir, nil, binary, "pack", source, "extra")
		if err == nil || !strings.Contains(stderr, "expects one source") {
			t.Fatalf("pack accepted program arguments: %v\n%s", err, stderr)
		}
	})
}
