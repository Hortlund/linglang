package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

// Use a small OTP fixture to test timing boundaries and cleanup without adding
// another full compilation of the bootstrap compiler to every CI push.
func TestFrontendMeasurement(t *testing.T) {
	requireOTP(t)
	dir := t.TempDir()
	sources := compiler.RuntimeSources()
	for _, variant := range []string{"optimized", "cells"} {
		sources["frontend_"+variant+".erl"] = fmt.Sprintf(`
-module(frontend_%s).
-export([f_%x/1, f_%x/2]).
f_%x(<<"parse">>) ->
 linglang_rt:scope(fun() -> [linglang_rt:new(N) || N <- lists:seq(1, 1000)] end),
 17.
f_%x(<<"parse">>, 17) ->
 linglang_rt:scope(fun() -> [linglang_rt:new(N) || N <- lists:seq(1, 3)] end),
 42.
`, variant, "benchmarkPrepare", "benchmarkRun", "benchmarkPrepare", "benchmarkRun")
	}
	args := []string{"-o", dir}
	for name, source := range sources {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		args = append(args, path)
	}
	if _, err := execute(20*time.Second, "erlc", args...); err != nil {
		t.Fatal(err)
	}
	script := frontendMeasurementScript("parse", 2, 1)
	data, err := execute(20*time.Second, "erl", "+S", "1", "-noshell", "-pa", dir, "-eval", script)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "OTP:") || strings.HasPrefix(line, "VM:") {
			continue
		}
		var s sample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			t.Fatal(err)
		}
		if s.Workload != "parse" || s.Allocations != 3 || s.PeakCells != 3 || s.Collections != 0 || s.Reductions <= 0 || s.ResultCount == nil || *s.ResultCount != 42 {
			t.Fatalf("preparation contaminated phase counters or inputs/results changed: %+v", s)
		}
		count++
	}
	if count != 4 {
		t.Fatalf("got %d samples, want 4 (excluding warmups)", count)
	}
	// Failure must propagate instead of silently recording an invalid phase.
	script = strings.Replace(script, "Count > 0", "Count > 42", 1)
	if _, err := execute(20*time.Second, "erl", "+S", "1", "-noshell", "-pa", dir, "-eval", script); err == nil || !strings.Contains(err.Error(), "benchmark_failed") {
		t.Fatalf("invalid phase result wasn't rejected: %v", err)
	}
}

func TestFrontendSources(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.lang", "a.lang", "a_test.lang", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := frontendSources(dir, ".")
	if err != nil || len(sources) != 2 || sources[0].Filename != "a.lang" || sources[1].Filename != "b.lang" || string(sources[0].Source) != "a.lang" {
		t.Fatalf("unexpected corpus: %+v (%v)", sources, err)
	}
	if _, err := frontendSources(dir, "missing"); err == nil {
		t.Fatal("accepted missing corpus")
	}
}
