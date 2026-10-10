package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

// Use a tiny archive to exercise the profiler's process boundary, arguments,
// tracing and failure handling without rebuilding the compiler in every CI run.
func TestArchiveProfiler(t *testing.T) {
	requireOTP(t)
	escript, err := exec.LookPath("escript")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	sources := compiler.RuntimeSources()
	sources["linglang_program.erl"] = `-module(linglang_program).
-export([main/0, f_776f726b/0]).
main() ->
 [<<"check">>, Source | Flags] = linglang_rt:arguments(),
 case filename:basename(Source) of
  <<"slow">> -> receive never -> ok end;
  <<"broken">> -> exit(fixture_failed);
  <<"leak">> -> [] = Flags;
  <<"cells">> -> [<<"--no-opt">>] = Flags;
  <<"ok">> -> [] = Flags
 end,
 linglang_rt:scope(fun() -> [?MODULE:f_776f726b() || _ <- lists:seq(1, 3)] end),
 case filename:basename(Source) of <<"leak">> -> ok; _ -> linglang_rt:collect() end,
 ok.
f_776f726b() -> linglang_rt:new(42).
`
	args := []string{"-o", dir}
	for name, source := range sources {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		args = append(args, path)
	}
	if _, err := execute(30*time.Second, "erlc", args...); err != nil {
		t.Fatal(err)
	}
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	for _, module := range []string{"linglang_program", "linglang_rt", "linglang_sup", "linglang_server", "linglang_io"} {
		name := module + ".beam"
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	archive := append([]byte("#!/usr/bin/env escript\n%% fixture\n%%! +S 1\n"), zipped.Bytes()...)
	artifact := filepath.Join(dir, "compiler.escript")
	if err := os.WriteFile(artifact, archive, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, failure string
		extra         []string
	}{
		{name: "ok"},
		{name: "cells", extra: []string{"--no-opt"}},
		{name: "broken", failure: "fixture_failed"},
		{name: "leak", failure: "live_cells"},
		{name: "slow", extra: []string{"--timeout", "1"}, failure: "timeout"},
		{name: "bad-argument", extra: []string{"--unknown"}, failure: "invalid_arguments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			source := filepath.Join(dir, tc.name)
			args := append([]string{"../../benchmarks/profile.escript", "--compiler", artifact, "--source", source}, tc.extra...)
			cmd := exec.CommandContext(ctx, escript, args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if ctx.Err() != nil {
				t.Fatalf("profiler did not terminate: %v", ctx.Err())
			}
			if tc.failure != "" {
				if err == nil || !strings.Contains(stderr.String(), tc.failure) || bytes.Contains(stdout.Bytes(), []byte(`"schemaVersion"`)) {
					t.Fatalf("expected %s failure: %v\nstdout: %s\nstderr: %s", tc.failure, err, &stdout, &stderr)
				}
				return
			}
			if err != nil {
				t.Fatalf("profile: %v\n%s\n%s", err, &stdout, &stderr)
			}
			var report struct {
				Version    int    `json:"schemaVersion"`
				Compiler   string `json:"compiler"`
				SHA256     string `json:"compiler_sha256"`
				Source     string `json:"source"`
				NoOpt      bool   `json:"no_opt"`
				Schedulers int    `json:"schedulers"`
				Managed    struct {
					Allocated int `json:"allocated_cells"`
					Live      int `json:"live_cells"`
					Frames    int `json:"root_frames"`
					Entries   int `json:"root_entries"`
				} `json:"managed"`
				Functions []struct {
					Module   string `json:"module"`
					Function string `json:"source_function"`
					Calls    int    `json:"calls"`
				} `json:"functions"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatalf("invalid JSON report: %v\n%s", err, &stdout)
			}
			digest := sha256.Sum256(archive)
			if report.Version != 1 || report.Compiler != artifact || report.SHA256 != hex.EncodeToString(digest[:]) || report.Source != source || report.NoOpt != (tc.name == "cells") || report.Schedulers != 1 {
				t.Fatalf("incorrect report metadata: %+v", report)
			}
			if report.Managed.Allocated != 3 || report.Managed.Live != 0 || report.Managed.Frames != 0 || report.Managed.Entries != 0 {
				t.Fatalf("incorrect allocation/cleanup counters: %+v", report.Managed)
			}
			for _, row := range report.Functions {
				if row.Module == "linglang_program" && row.Function == "work" && row.Calls == 3 {
					return
				}
			}
			t.Fatalf("missing traced source function: %+v", report.Functions)
		})
	}
}
