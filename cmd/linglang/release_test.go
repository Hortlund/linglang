package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReleaseCLI(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("runtime release launcher targets Linux and macOS")
	}
	binary := filepath.Join(t.TempDir(), "linglang")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	testReleaseDriver(t, binary, nil, false)
}

func testBootstrapRelease(t *testing.T, cli string, env []string) {
	testReleaseDriver(t, cli, env, true)
}

func testReleaseDriver(t *testing.T, binary string, buildEnv []string, bootstrap bool) {
	// There is nothing on PATH at runtime: startup must use the bundled VM and
	// system utilities directly, without finding Go or Erlang through PATH.
	var isolatedEnv []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "PATH", "ERL_LIBS", "ERL_FLAGS", "ERL_AFLAGS", "ERL_ZFLAGS", "ROOTDIR", "BINDIR", "EMU", "PROGNAME":
		default:
			isolatedEnv = append(isolatedEnv, entry)
		}
	}
	isolatedEnv = append(isolatedEnv, "PATH="+t.TempDir())
	invoke := func(dir string, env []string, command string, args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Dir, cmd.Env = dir, env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}
	makeRelease := func(t *testing.T, source, work string, baseline bool) string {
		t.Helper()
		artifact := filepath.Join(work, "out", "program.tar.gz")
		args := []string{"release", "--gc-stress", "--gc-stats", "-o", artifact}
		if baseline {
			args = append(args, "--no-opt")
		}
		args = append(args, source)
		out, stderr, err := invoke(work, buildEnv, binary, args...)
		if err != nil || !strings.Contains(out, "Built runtime release in "+artifact) {
			t.Fatalf("release: %v\n%s\n%s", err, out, stderr)
		}
		// Extract to a different location, with spaces and Unicode in its path.
		moved := filepath.Join(t.TempDir(), "雪 bean moved")
		unpackRelease(t, artifact, moved)
		for _, path := range []string{"bin/run", "releases/1/start.boot", "lib/linglang-1/ebin/linglang.app", "OTP_LICENSE.txt", "README.txt"} {
			if _, err := os.Stat(filepath.Join(moved, path)); err != nil {
				t.Fatal(err)
			}
		}
		if matches, _ := filepath.Glob(filepath.Join(moved, "erts-*", "bin", "beam.smp")); len(matches) != 1 {
			t.Fatalf("missing bundled VM: %v", matches)
		}
		license, err := os.ReadFile(filepath.Join(moved, "OTP_LICENSE.txt"))
		if err != nil || string(license) != otpLicense {
			t.Fatalf("OTP license differs: %v", err)
		}
		return moved
	}
	for _, baseline := range []bool{false, true} {
		name := "optimized"
		if baseline {
			name = "baseline"
		}
		t.Run(name, func(t *testing.T) {
			t.Run("stdin", func(t *testing.T) {
				work := t.TempDir()
				source := filepath.Join(work, "stdin.lang")
				program := `package main
func main(){var data string;for{input:=readFile("/dev/stdin");assert(input.ok);if len(input.value)==0{break};data+=input.value};assert(writeFile(head(args()).value,data).ok);println(len(data))}`
				if err := os.WriteFile(source, []byte(program), 0600); err != nil {
					t.Fatal(err)
				}
				bundle := makeRelease(t, source, work, baseline)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				saved := filepath.Join(work, "stdin.bytes")
				cmd := exec.CommandContext(ctx, filepath.Join(bundle, "bin", "run"), saved)
				cmd.Env, cmd.Dir = isolatedEnv, t.TempDir()
				input := strings.Repeat("snow 雪\x00\xff\n", 10000)
				cmd.Stdin = strings.NewReader(input)
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				if err := cmd.Run(); err != nil || stdout.String() != strconv.Itoa(len(input))+"\n" || !strings.Contains(stderr.String(), "live_cells => 0") {
					t.Fatalf("release stdin: %v, %d output bytes\n%s", err, stdout.Len(), stderr.String())
				}
				data, err := os.ReadFile(saved)
				if err != nil || string(data) != input {
					t.Fatalf("stdin bytes changed: %v, %d bytes", err, len(data))
				}
			})
			t.Run("arguments_workers_and_failures", func(t *testing.T) {
				work := t.TempDir()
				source := filepath.Join(work, "beans.lang")
				program := `package main
type Reply struct { values List[string] }
func worker(parent Pid) { send(parent, Reply{values: args()}); receive[int](-1) }
func crashing(n int) { panic("release worker boom") }
func bracketArgs(values List[string]) string {
    result := ""
    for _, value := range values { result += "[" + value + "]" }
    return result
}
func main() {
    values := args()
    if head(values).ok && head(values).value == "panic" { panic("release main boom") }
    if head(values).ok && head(values).value == "escalate" {
        supervisor := startSupervisor(0, 5)
        supervise(supervisor, "crashing", crashing, 0, ChildOptions{restart: "permanent"})
        receive[int](-1)
        return
    }
    println(len(values), bracketArgs(values))
    supervisor := startSupervisor(3, 5)
    child := supervise(supervisor, "worker", worker, self(), ChildOptions{restart: "permanent"})
    if !child.ok { panic(child.reason) }
    reply := receive[Reply](5000)
    if !reply.ok || bracketArgs(reply.value.values) != bracketArgs(values) { panic("worker arguments differ") }
    if !stopChild(supervisor, "worker") { panic("cannot stop worker") }
    replacement := restartChild(supervisor, "worker")
    if !replacement.ok || replacement.pid == child.pid { panic("cannot restart worker") }
    reply = receive[Reply](5000)
    if !reply.ok || bracketArgs(reply.value.values) != bracketArgs(values) { panic("restart arguments differ") }
    if !stopSupervisor(supervisor) { panic("cannot stop supervisor") }
    println("workers ok")
}`
				if err := os.WriteFile(source, []byte(program), 0644); err != nil {
					t.Fatal(err)
				}
				bundle := makeRelease(t, source, work, baseline)
				if err := os.RemoveAll(work); err != nil {
					t.Fatal(err)
				}
				cwd := t.TempDir()
				runner := filepath.Join(bundle, "bin", "run")
				links := filepath.Join(cwd, "雪 aliases")
				if err := os.Mkdir(links, 0755); err != nil {
					t.Fatal(err)
				}
				absoluteLink := filepath.Join(links, "absolute bean")
				if err := os.Symlink(runner, absoluteLink); err != nil {
					t.Fatal(err)
				}
				relativeLink := filepath.Join(links, "relative bean")
				target, err := filepath.Rel(links, runner)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, relativeLink); err != nil {
					t.Fatal(err)
				}
				chainedLink := filepath.Join(cwd, "chained bean")
				if err := os.Symlink(filepath.Join("雪 aliases", "relative bean"), chainedLink); err != nil {
					t.Fatal(err)
				}
				for _, runner := range []string{runner, absoluteLink, relativeLink, chainedLink, filepath.Join("雪 aliases", "relative bean"), "./chained bean"} {
					for _, args := range [][]string{nil, {"雪 beans", "--gc-stress", "-extra", "\"quoted\"", ""}} {
						out, stderr, err := invoke(cwd, isolatedEnv, runner, args...)
						want := "0 \nworkers ok\n"
						if len(args) > 0 {
							want = "5 [雪 beans][--gc-stress][-extra][\"quoted\"][]\nworkers ok\n"
						}
						if err != nil || out != want || !strings.Contains(stderr, "live_cells => 0") || !strings.Contains(stderr, "root_frames => 0") {
							t.Fatalf("run release via %s: %v\nstdout: %q\nstderr: %s", runner, err, out, stderr)
						}
					}
				}
				// A user can install just a symlink on PATH. The shell must find the
				// alias, then the launcher must find its bundle without PATH tools.
				pathEnv := append([]string(nil), isolatedEnv...)
				pathEnv[len(pathEnv)-1] = "PATH=" + links
				out, stderr, err := invoke(cwd, pathEnv, "/bin/sh", "-c", "exec \"$0\" \"$@\"", "relative bean", "雪", "-extra", "")
				if err != nil || out != "3 [雪][-extra][]\nworkers ok\n" || !strings.Contains(stderr, "live_cells => 0") {
					t.Fatalf("run release via PATH: %v\nstdout: %q\nstderr: %s", err, out, stderr)
				}
				for _, arg := range []string{"panic", "escalate"} {
					out, stderr, err := invoke(cwd, isolatedEnv, runner, arg)
					exit, ok := err.(*exec.ExitError)
					if !ok || exit.ExitCode() != 1 {
						t.Fatalf("failure exit (%s): %v\n%s\n%s", arg, err, out, stderr)
					}
					if arg == "panic" && !strings.Contains(stderr, "release main boom") {
						t.Fatalf("missing exception diagnostic: %s", stderr)
					}
					if arg == "escalate" && !strings.Contains(stderr, "linglang process exited") {
						t.Fatalf("missing supervisor failure diagnostic: %s", stderr)
					}
				}
			})
			t.Run("prime_lab", func(t *testing.T) {
				work := t.TempDir()
				source, err := filepath.Abs("../../examples/prime_lab")
				if err != nil {
					t.Fatal(err)
				}
				bundle := makeRelease(t, source, work, baseline)
				cwd := t.TempDir()
				if err := os.WriteFile(filepath.Join(cwd, "jobs.txt"), []byte("100\n1000\n10000\n"), 0644); err != nil {
					t.Fatal(err)
				}
				out, stderr, err := invoke(cwd, isolatedEnv, filepath.Join(bundle, "bin", "run"), "jobs.txt", "results.csv")
				if err != nil || !strings.Contains(out, "Completed: 3 | Worker restarts: 1") || !strings.Contains(out, "Supervisor stopped: true") || !strings.Contains(stderr, "live_cells => 0") {
					t.Fatalf("prime lab: %v\n%s\n%s", err, out, stderr)
				}
				data, err := os.ReadFile(filepath.Join(cwd, "results.csv"))
				want := "job,limit,prime_count,largest_prime\n1,100,25,97\n2,1000,168,997\n3,10000,1229,9973\n"
				if err != nil || string(data) != want {
					t.Fatalf("CSV: %v\n%s", err, data)
				}
			})
		})
	}
	t.Run("output_and_failures", func(t *testing.T) {
		work := t.TempDir()
		source := filepath.Join(work, "bean.lang")
		original := []byte("package main\nfunc main() { println(42) }\n")
		if err := os.WriteFile(source, original, 0644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := invoke(work, buildEnv, binary, "release", source)
		archive := filepath.Join(work, "bean-"+runtime.GOOS+"-"+runtime.GOARCH+".tar.gz")
		if bootstrap {
			archive = filepath.Join(work, "program.tar.gz")
		}
		if err != nil || !strings.Contains(out, filepath.Base(archive)) {
			t.Fatalf("default output: %v\n%s\n%s", err, out, stderr)
		}
		_, stderr, err = invoke(work, buildEnv, binary, "release", "-o", source, source)
		if err == nil || !strings.Contains(stderr, "refusing to overwrite source") {
			t.Fatalf("source overwrite allowed: %v\n%s", err, stderr)
		}
		data, err := os.ReadFile(source)
		if err != nil || !bytes.Equal(data, original) {
			t.Fatal("source was changed")
		}
		old, err := os.ReadFile(archive)
		if err != nil {
			t.Fatal(err)
		}
		if bootstrap {
			testReleaseFailures(t, binary, buildEnv, work, source, archive, old)
			return
		}
		_, stderr, err = invoke(work, isolatedEnv, binary, "release", "-o", archive, source)
		if err == nil || !strings.Contains(stderr, "erl is not on PATH") {
			t.Fatalf("missing OTP: %v\n%s", err, stderr)
		}
		data, err = os.ReadFile(archive)
		if err != nil || !bytes.Equal(data, old) {
			t.Fatal("failed release changed existing archive")
		}
	})
}

func testReleaseFailures(t *testing.T, cli string, env []string, work, source, archive string, old []byte) {
	t.Helper()
	invoke := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, cli, args...)
		cmd.Env, cmd.Dir = env, work
		if out, err := cmd.CombinedOutput(); err == nil || ctx.Err() != nil {
			t.Fatalf("expected prompt failure for %v: %v\n%s", args, err, out)
		}
		data, err := os.ReadFile(archive)
		if err != nil || !bytes.Equal(data, old) {
			t.Fatal("failed release changed the existing archive")
		}
	}
	for _, args := range [][]string{
		{"release", "--json", source}, {"release", "--timeout", "1s", source},
		{"release", "--tests", source}, {"release", "-o"}, {"release", source, "--", "arg"},
		{"release", "-o", archive, "missing.lang"},
	} {
		invoke(args...)
	}
	bad := filepath.Join(work, "bad.lang")
	if err := os.WriteFile(bad, []byte("package main\nfunc main(){ missing() }"), 0600); err != nil {
		t.Fatal(err)
	}
	invoke("release", "-o", archive, bad)
	// Protection includes imported sources and aliases, not just the root input.
	dep := filepath.Join(work, "dep")
	if err := os.Mkdir(dep, 0700); err != nil {
		t.Fatal(err)
	}
	dependency := filepath.Join(dep, "value.lang")
	original := []byte("package dep\nfunc Value() int { return 42 }\n")
	if err := os.WriteFile(dependency, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("package main\nimport \"./dep\"\nfunc main(){println(dep.Value())}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(work, "alias.tar.gz")
	hard := filepath.Join(work, "hard.tar.gz")
	if err := os.Symlink(dependency, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(dependency, hard); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{dependency, link, hard} {
		invoke("release", "-o", target, source)
		data, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(data, original) {
			t.Fatal("protected source changed")
		}
	}
	// Reach publication, then fail renaming over a directory. No staging files
	// should remain alongside the destination after the handled failure.
	target := filepath.Join(work, "occupied")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	invoke("release", "-o", target, source)
	if files, _ := filepath.Glob(filepath.Join(work, ".linglang-*")); len(files) != 0 {
		t.Fatalf("staging files leaked: %v", files)
	}
}

func unpackRelease(t *testing.T, path, destination string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsLocal(header.Name) {
			t.Fatalf("invalid archive path: %s", header.Name)
		}
		path := filepath.Join(destination, header.Name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(header.Mode)); err != nil {
				t.Fatal(err)
			}
		case tar.TypeReg:
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, os.FileMode(header.Mode)); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unexpected archive entry %s (%d)", header.Name, header.Typeflag)
		}
	}
}
