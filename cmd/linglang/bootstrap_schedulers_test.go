package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestBootstrapSchedulerDefaults(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bootstrap launcher targets Linux/macOS")
	}
	dir := t.TempDir()
	source := "-module(linglang_program). -export([main/0]). main() -> io:format(\"~p~n\", [erlang:system_info(schedulers_online)])."
	if err := build(dir, source); err != nil {
		t.Fatal(err)
	}
	env := isolatedOTPEnvironment(t, filepath.Join(t.TempDir(), "otp"), false)
	// ERL_AFLAGS precedes CLI flags. The old application's hard-coded +S 1
	// overrides this setting; a healthy artifact respects the operator's +S 2.
	env = append(env, "ERL_AFLAGS=+S 2:2")
	invoke := func(name string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		return string(out)
	}
	artifact := filepath.Join(t.TempDir(), "application")
	eval := `[SourcePath, Output] = init:get_plain_arguments(), {ok, Source} = file:read_file(SourcePath),
#{field_6f6b := true} = linglang_rt:build_program(Source, unicode:characters_to_binary(Output), nil, false, false),
#{field_6f6b := true} = linglang_rt:run_program(Source, nil, false, false), halt(0).`
	// Deliberately use one scheduler in the parent compiler. Neither run nor
	// build may bake that compiler-stage policy into the application child VM.
	if out := invoke("erl", "+S", "1", "-noshell", "-pa", dir, "-eval", eval, "-extra", filepath.Join(dir, "linglang_program.erl"), artifact); out != "2\n" {
		t.Fatalf("run child scheduler count: %q", out)
	}
	if out := invoke("escript", artifact); out != "2\n" {
		t.Fatalf("packaged application scheduler count: %q", out)
	}
}
