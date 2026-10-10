package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Small deterministic compiler double checks orchestration failures cheaply.
// The real compiler fixed point and language suites remain an explicit run of
// tools/bootstrap.escript; this test does not claim a self-hosting proof.
func TestGoFreeBootstrapTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Go-free bootstrap targets Linux/macOS")
	}
	tool, err := filepath.Abs("../../tools/bootstrap.escript")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "compiler's 雪 tree")
	for _, directory := range []string{"bootstrap/emitter", "bootstrap/seed-upgrade", "internal/compiler", "bin"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"runtime.erl", "supervision.erl", "server.erl", "io.erl"} {
		data, err := os.ReadFile(filepath.Join("../../internal/compiler", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "internal/compiler", name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	fixture := filepath.Join(root, "bootstrap/emitter/compiler.erl")
	var octets []string
	for _, b := range []byte(fixture) {
		octets = append(octets, strconv.Itoa(int(b)))
	}
	module := "-module(linglang_program).\n-export([main/0]).\nmain() -> case linglang_rt:arguments() of [<<\"emit\">>|_] -> {ok, Source}=file:read_file(<<" + strings.Join(octets, ",") + ">>), io:put_chars(Source); _ -> io:put_chars(\"fixture suite passed\\n\") end.\n"
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(fixture, module)
	seed := filepath.Join(root, "bin/seed")
	if err := pack(seed, module, false, false); err != nil {
		t.Fatal(err)
	}
	env := isolatedOTPEnvironment(t, filepath.Join(t.TempDir(), "otp"), false)
	work := t.TempDir()
	env = append(env, "TMPDIR="+work)
	stageRunning := func() bool {
		t.Helper()
		out, err := exec.Command("ps", "-axo", "stat=,command=").Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && !strings.HasPrefix(fields[0], "Z") && strings.Contains(line, filepath.Join(work, "linglang-bootstrap-")) {
				return true
			}
		}
		return false
	}
	waitStopped := func() {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for stageRunning() {
			if time.Now().After(deadline) {
				t.Fatal("orphaned compiler stage")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	invoke := func(args ...string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "escript", append([]string{tool, "--root", root, "--seed", "bin/seed", "--output", "bin/result"}, args...)...)
		command.Env = env
		out, err := command.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("tool hung: %s", out)
		}
		return string(out), err
	}
	output := filepath.Join(root, "bin/result")
	if out, err := invoke("--upgrade-seed"); err != nil || !strings.Contains(out, "Running upgrade-seed") {
		t.Fatalf("upgrade orchestration: %v %s", err, out)
	}
	if out, err := invoke(); err != nil || !strings.Contains(out, "Fixed point:") || !strings.Contains(out, "Verified compiler published:") {
		t.Fatalf("successful rebuild: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "escript", output, "test")
	command.Env = env
	if out, err := command.CombinedOutput(); err != nil || string(out) != "fixture suite passed\n" {
		t.Fatalf("published archive: %v\n%s", err, out)
	}
	before, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, source, want string }{
		{"mismatch", "-module(linglang_program). -export([main/0]). main() -> io:put_chars(\"different\").", "compiler_fixed_point_mismatch"},
		{"failed_suite", strings.Replace(module, "io:put_chars(\"fixture suite passed\\n\")", "erlang:error(suite_failed)", 1), "stage_failed"},
		{"timeout", "-module(linglang_program). -export([main/0]). main() -> receive never -> ok end.", "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write(fixture, tc.source)
			out, err := invoke("--timeout", "1")
			if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("want %s: %v\n%s", tc.want, err, out)
			}
			after, err := os.ReadFile(output)
			if err != nil || string(after) != string(before) {
				t.Fatal("failed rebuild replaced working artifact")
			}
			waitStopped()
		})
	}
	// Kill only the orchestration VM while B loops; its independent fd3 guard
	// must terminate B without needing stdin, a signal to the group, or timeout.
	write(fixture, "-module(linglang_program). -export([main/0]). main() -> receive never -> ok end.")
	parent := exec.Command("escript", tool, "--root", root, "--seed", "bin/seed", "--output", "bin/result")
	parent.Env = env
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		stages, _ := filepath.Glob(filepath.Join(work, "linglang-bootstrap-*", "b", "linglang_program.beam"))
		if len(stages) > 0 && stageRunning() {
			break
		}
		if time.Now().After(deadline) {
			parent.Process.Kill()
			parent.Wait()
			t.Fatal("compiler B did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	parent.Process.Signal(syscall.SIGTERM)
	if err := parent.Wait(); err == nil {
		t.Fatal("interrupted build reported success")
	}
	waitStopped()
	if after, err := os.ReadFile(output); err != nil || string(after) != string(before) {
		t.Fatal("cancelled rebuild replaced working artifact")
	}
	write(fixture, module)
	upgradeSource := filepath.Join(root, "bootstrap/seed-upgrade/developer_tools.lang")
	write(upgradeSource, "preserve upgrade source")
	protected := filepath.Join(root, "bootstrap/emitter/main.lang")
	write(protected, "preserve me")
	alias := filepath.Join(root, "bin/source-alias")
	if err := os.Link(protected, alias); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{seed, protected, upgradeSource, alias, filepath.Join(root, "internal/compiler/runtime.erl")} {
		out, err := invoke("--output", target)
		if err == nil || !strings.Contains(out, "refusing_to_overwrite_input") {
			t.Fatal(fmt.Sprintf("source protection %s: %v\n%s", target, err, out))
		}
	}
}
