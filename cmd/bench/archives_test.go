package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCompilerFlags(t *testing.T) {
	var flags compilerFlags
	if err := flags.Set("before=/a path/compiler=one"); err != nil {
		t.Fatal(err)
	}
	if err := flags.Set("after=/another"); err != nil {
		t.Fatal(err)
	}
	want := append(compilerFlags(nil), flags...)
	for _, invalid := range []string{"missing", "=path", "before=duplicate", "UPPER=path", "a-b=path", "x", "x=", "x');halt(1)=path"} {
		if err := flags.Set(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
		if !reflect.DeepEqual(flags, want) {
			t.Fatal("invalid flag changed configuration")
		}
	}
}

// An OTP-only archive double exercises command selection, paths, mode flags,
// stderr separation, rotated variants, runtime execution and retained hashes.
// Real compiler coverage is opt-in below and supplied by the recorded baseline.
const applicationArchiveFixture = `#!/usr/bin/env escript
main(["describe", "--json"]) -> io:put_chars("{\"schemaVersion\":1,\"compiler\":\"bootstrap\"}");
main(["emit" | Args]) ->
    Path = lists:last(Args),
    {ok, Source} = file:read_file(Path),
    true = byte_size(Source) > 0,
    io:format(standard_error, "fixture diagnostic~n", []),
    Body = case Args of
        ["--no-opt", Path] -> "linglang_rt:scope(fun() -> linglang_rt:new(7), ok end)";
        [Path] -> "ok"
    end,
    io:put_chars("-module(linglang_program).\n-export([main/0]).\nmain() -> " ++ Body ++ ".\n").
`

func writeApplicationArchive(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "compiler ' λ.escript")
	if err := os.WriteFile(path, []byte(source), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArchiveApplications(t *testing.T) {
	requireOTP(t)
	archive := writeApplicationArchive(t, applicationArchiveFixture)
	dir := t.TempDir()
	got, err := runBench(config{
		samples: 3, warmup: 1, workloads: []string{"arithmetic"},
		sourceDir: "../../benchmarks", dir: dir, output: filepath.Join(dir, "result.json"),
		compilers: compilerFlags{{Label: "first", Path: archive}, {Label: "second", Path: archive}},
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || len(got.Compilers) != 2 || len(got.Inputs) != 2 || len(got.Generated) == 0 || len(got.Samples) != 21 {
		t.Fatalf("incomplete report: %+v", got)
	}
	for _, c := range got.Compilers {
		data, err := os.ReadFile(c.Snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if c.Path != archive || c.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) || string(data) != applicationArchiveFixture {
			t.Fatalf("incorrect archive evidence: %+v", c)
		}
	}
	for _, f := range append(got.Inputs, got.Generated...) {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		if f.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
			t.Fatalf("bad hash: %+v", f)
		}
		if f.Snapshot != "" {
			frozen, err := os.ReadFile(f.Snapshot)
			if err != nil || string(data) != string(frozen) {
				t.Fatalf("input snapshot differs: %+v: %v", f, err)
			}
		}
	}
	for round := 0; round < 3; round++ {
		for i := 0; i < len(got.Variants); i++ {
			s := got.Samples[round*len(got.Variants)+i]
			if s.Variant != got.Variants[(round+1+i)%len(got.Variants)] {
				t.Fatalf("wrong rotation: %+v", got.Samples)
			}
			if strings.HasSuffix(s.Variant, "_cells") && s.Allocations != 1 {
				t.Fatalf("cell flag not honored: %+v", s)
			}
			if strings.HasSuffix(s.Variant, "_optimized") && s.Allocations != 0 {
				t.Fatalf("unexpected allocations: %+v", s)
			}
		}
	}
}

func TestArchiveApplicationFailures(t *testing.T) {
	requireOTP(t)
	for _, tc := range []struct{ name, source, errorText string }{
		{"describe", strings.Replace(applicationArchiveFixture, "bootstrap", "go-seed", 1), "expected bootstrap"},
		{"exit", strings.Replace(applicationArchiveFixture, "Path = lists:last(Args)", "halt(7), Path = lists:last(Args)", 1), "exit status 7"},
		{"module", strings.Replace(applicationArchiveFixture, "-module(linglang_program)", "-module(other)", 1), "one linglang_program module"},
		{"nondeterministic", strings.Replace(applicationArchiveFixture, "Path = lists:last(Args),", "io:format(\"%% ~p~n\", [erlang:system_time(nanosecond)]), Path = lists:last(Args),", 1), "nondeterministic"},
		{"assertion", strings.Replace(applicationArchiveFixture, "[Path] -> \"ok\"", "[Path] -> \"error(wrong_answer)\"", 1), "benchmark_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := writeApplicationArchive(t, tc.source)
			dir := t.TempDir()
			output := filepath.Join(dir, "result.json")
			if err := os.WriteFile(output, []byte("old report"), 0644); err != nil {
				t.Fatal(err)
			}
			_, err := runBench(config{samples: 1, workloads: []string{"arithmetic"}, sourceDir: "../../benchmarks", dir: dir, output: output,
				compilers: compilerFlags{{Label: "broken", Path: archive}}}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.errorText) {
				t.Fatalf("got %v, want %s", err, tc.errorText)
			}
			data, err := os.ReadFile(output)
			if err != nil || string(data) != "old report" {
				t.Fatal("failed benchmark replaced existing report")
			}
		})
	}
}

func TestSelfHostedApplicationWorkloads(t *testing.T) {
	archive := os.Getenv("LINGLANG_BENCH_COMPILER")
	if archive == "" {
		t.Skip("set LINGLANG_BENCH_COMPILER to a verified self-hosted archive")
	}
	requireOTP(t)
	dir := t.TempDir()
	_, err := runBench(config{samples: 1, warmup: 1, workloads: workloads, sourceDir: "../../benchmarks", dir: dir, output: filepath.Join(dir, "result.json"),
		compilers: compilerFlags{{Label: "bootstrap", Path: archive}}}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
}

func TestArchiveComparisons(t *testing.T) {
	requireComparisonTools(t)
	archive := writeApplicationArchive(t, applicationArchiveFixture)
	dir := t.TempDir()
	got, err := runBench(config{samples: 3, warmup: 1, workloads: []string{"arithmetic"}, sourceDir: "../../benchmarks", dir: dir, output: filepath.Join(dir, "comparison.json"), compare: true,
		compilers: compilerFlags{{Label: "selfhost", Path: archive}}}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]string(nil), comparisonVariants...), "selfhost_optimized", "selfhost_cells")
	if !reflect.DeepEqual(got.Variants, want) || len(got.Samples) != 18 || len(got.Summaries) != 6 || len(got.Inputs) != 4 || got.Elixir == "" || got.NativeGo == "" {
		t.Fatalf("incomplete combined comparison: %+v", got)
	}
	beam := []string{"optimized", "erlang", "elixir", "selfhost_optimized", "selfhost_cells"}
	for round := 0; round < 3; round++ {
		for i := range beam {
			sample := got.Samples[round*len(beam)+i]
			if sample.Variant != beam[(round+1+i)%len(beam)] {
				t.Fatalf("wrong BEAM rotation: %+v", got.Samples)
			}
			if sample.Variant == "selfhost_cells" && sample.Allocations != 1 {
				t.Fatalf("cell mode missing: %+v", sample)
			}
		}
	}
	for _, sample := range got.Samples[15:] {
		if sample.Variant != "go" || sample.GoAllocations == nil {
			t.Fatalf("missing Go sample: %+v", sample)
		}
	}
}
