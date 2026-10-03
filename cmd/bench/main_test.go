package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSummary(t *testing.T) {
	for _, tc := range []struct {
		times  []int
		median float64
		p95    int
	}{
		{[]int{9}, 9, 9},
		{[]int{9, 2, 4}, 4, 9},
		{[]int{9, 2, 4, 5}, 4.5, 9},
		{[]int{20, 1, 19, 2, 18, 3, 17, 4, 16, 5, 15, 6, 14, 7, 13, 8, 12, 9, 11, 10}, 10.5, 19},
	} {
		var group []sample
		for _, micros := range tc.times {
			group = append(group, sample{Workload: "arithmetic", Variant: "optimized", Microseconds: micros})
		}
		got := summarize(group)
		if got.Workload != "arithmetic" || got.Variant != "optimized" || got.MedianMicros != tc.median || got.P95Micros != tc.p95 {
			t.Fatalf("summarize(%v) = %+v, want median %v, p95 %d", tc.times, got, tc.median, tc.p95)
		}
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, cfg := range []config{
		{samples: 0, workloads: workloads},
		{samples: 101, workloads: workloads},
		{samples: 1, warmup: -1, workloads: workloads},
		{samples: 1, warmup: 101, workloads: workloads},
		{samples: 1},
		{samples: 1, workloads: []string{"unknown"}},
		{samples: 1, workloads: []string{"arithmetic", "arithmetic"}},
	} {
		if _, err := runBench(cfg, io.Discard); err == nil {
			t.Fatalf("accepted invalid config: %+v", cfg)
		}
	}
}

func requireOTP(t *testing.T) {
	t.Helper()
	for _, executable := range []string{"erl", "erlc"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Skipf("%s unavailable: %v", executable, err)
		}
	}
}

// Exercise all actual programs and references, including their correctness and
// root-cleanup assertions. Timing thresholds would make this a flaky CI test.
func TestBenchmarkWorkloads(t *testing.T) {
	requireOTP(t)
	dir := t.TempDir()
	cfg := config{samples: 1, warmup: 1, workloads: workloads, sourceDir: "../../benchmarks", dir: dir, output: filepath.Join(dir, "results.json")}
	var log bytes.Buffer
	got, err := runBench(cfg, &log)
	if err != nil {
		t.Fatalf("benchmark: %v\n%s", err, log.String())
	}
	if len(got.Samples) != len(workloads)*len(variants) || len(got.Summaries) != len(got.Samples) {
		t.Fatalf("incorrect result count (warmups must be excluded): %+v", got)
	}
	if got.OTP == "" || got.VM == "" || got.Schedulers != 1 || got.WarmupPerVariant != 1 || len(got.SourceSHA256) != 64 {
		t.Fatalf("missing environment metadata: %+v", got)
	}
	for _, result := range got.Samples {
		if result.Microseconds < 0 || result.Reductions <= 0 || result.Collections < 0 {
			t.Fatalf("invalid measurement: %+v", result)
		}
		if result.Variant == "optimized" {
			want := 0
			if result.Workload == "pointers" {
				want = 1
			}
			if result.Allocations != want {
				t.Fatalf("unexpected managed allocations: %+v, want %d", result, want)
			}
		}
	}
	data, err := os.ReadFile(cfg.output)
	if err != nil {
		t.Fatal(err)
	}
	var saved report
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*got, saved) {
		t.Fatal("saved report differs from measured results")
	}
}

func TestIncorrectWorkloadFails(t *testing.T) {
	requireOTP(t)
	for _, broken := range []string{"lists.lang", "bench_reference.erl"} {
		t.Run(broken, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"lists.lang", "bench_reference.erl"} {
				data, err := os.ReadFile(filepath.Join("../../benchmarks", name))
				if err != nil {
					t.Fatal(err)
				}
				if name == broken {
					data = bytes.ReplaceAll(data, []byte("199990000"), []byte("199990001"))
				}
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			output := filepath.Join(dir, "results.json")
			_, err := runBench(config{samples: 1, workloads: []string{"lists"}, sourceDir: dir, dir: dir, output: output}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "benchmark_failed") {
				t.Fatalf("incorrect workload wasn't rejected: %v", err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("failed benchmark wrote a report")
			}
		})
	}
}

func requireComparisonTools(t *testing.T) {
	t.Helper()
	requireOTP(t)
	for _, executable := range []string{"go", "elixir"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Skipf("%s unavailable: %v", executable, err)
		}
	}
}

func TestBenchmarkComparisons(t *testing.T) {
	requireComparisonTools(t)
	dir := t.TempDir()
	got, err := runBench(config{
		samples: 2, workloads: workloads, sourceDir: "../../benchmarks", dir: dir,
		output: filepath.Join(dir, "comparison.json"), compare: true,
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got.NativeGo == "" || got.Elixir == "" || got.GoMaxProcs != 1 || !reflect.DeepEqual(got.Variants, comparisonVariants) {
		t.Fatalf("missing comparison metadata: %+v", got)
	}
	if len(got.Samples) != 2*len(workloads)*len(comparisonVariants) || len(got.Summaries) != len(workloads)*len(comparisonVariants) {
		t.Fatalf("incorrect comparison sample/summary counts: %d / %d", len(got.Samples), len(got.Summaries))
	}
	for _, result := range got.Samples {
		if result.Variant == "go" {
			if result.GoAllocations == nil || result.GoAllocatedBytes == nil {
				t.Fatalf("missing Go allocation counts: %+v", result)
			}
		} else if result.Reductions <= 0 {
			t.Fatalf("missing BEAM reduction counts: %+v", result)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "comparison.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved report
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*got, saved) {
		t.Fatal("saved comparison differs from measured results")
	}
}

func TestIncorrectComparisonFails(t *testing.T) {
	requireComparisonTools(t)
	for _, broken := range []string{"bench_reference.ex", "go/main.go"} {
		t.Run(broken, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"lists.lang", "bench_reference.erl", "bench_reference.ex", "go/main.go"} {
				data, err := os.ReadFile(filepath.Join("../../benchmarks", name))
				if err != nil {
					t.Fatal(err)
				}
				if name == broken {
					data = bytes.ReplaceAll(data, []byte("199990000"), []byte("199990001"))
					data = bytes.ReplaceAll(data, []byte("199_990_000"), []byte("199_990_001"))
				}
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			output := filepath.Join(dir, "comparison.json")
			_, err := runBench(config{samples: 1, workloads: []string{"lists"}, sourceDir: dir, dir: dir, output: output, compare: true}, io.Discard)
			if err == nil || (!strings.Contains(err.Error(), "benchmark_failed") && !strings.Contains(err.Error(), "incorrect benchmark result")) {
				t.Fatalf("incorrect comparison wasn't rejected: %v", err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("failed comparison wrote a report")
			}
		})
	}
}
