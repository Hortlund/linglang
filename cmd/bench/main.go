// bench compares the optimized backend, the cell backend, and handwritten Erlang.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"linglang/internal/compiler"
)

type sample struct {
	Workload     string `json:"workload"`
	Variant      string `json:"variant"`
	Microseconds int    `json:"microseconds"`
	Allocations  int    `json:"managed_allocations"`
	PeakCells    int    `json:"peak_managed_cells"`
	Reductions   int    `json:"reductions"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	samples := flag.Int("samples", 7, "measured runs per workload and variant")
	output := flag.String("out", "_build/benchmarks/results.json", "JSON result file")
	flag.Parse()
	if *samples < 1 || *samples > 100 {
		return fmt.Errorf("samples must be between 1 and 100")
	}
	dir, err := filepath.Abs("_build/benchmarks")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	sources := compiler.RuntimeSources()
	reference, err := os.ReadFile("benchmarks/bench_reference.erl")
	if err != nil {
		return fmt.Errorf("run this command from the repository root: %w", err)
	}
	sources["bench_reference.erl"] = string(reference)
	workloads := []string{"arithmetic", "calls", "structs", "pointers"}
	for _, name := range workloads {
		path := "benchmarks/" + name + ".lang"
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, variant := range []string{"optimized", "cells"} {
			program, err := compiler.CompileWithOptions(path, source, compiler.Options{DisableOptimizations: variant == "cells"})
			if err != nil {
				return err
			}
			module := "bench_" + name + "_" + variant
			sources[module+".erl"] = strings.Replace(program, "-module(linglang_program).", "-module("+module+").", 1)
		}
	}
	args := []string{"-o", dir}
	var names []string
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(sources[name]), 0644); err != nil {
			return err
		}
		args = append(args, path)
	}
	if output, err := exec.Command("erlc", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("erlc: %w\n%s", err, output)
	}
	var results []sample
	otp := ""
	for _, workload := range workloads {
		fmt.Println("Measuring", workload)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		data, err := exec.CommandContext(ctx, "erl", "+S", "1", "-noshell", "-pa", dir, "-eval", measurementScript(workload, *samples)).CombinedOutput()
		cancel()
		if err != nil {
			return fmt.Errorf("benchmark: %w\n%s", err, data)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.HasPrefix(line, "OTP:") {
				otp = strings.TrimPrefix(line, "OTP:")
				continue
			}
			var result sample
			if err := json.Unmarshal([]byte(line), &result); err != nil {
				return fmt.Errorf("invalid benchmark output %q: %w", line, err)
			}
			results = append(results, result)
		}
	}
	report := struct {
		Timestamp         string   `json:"timestamp"`
		Platform          string   `json:"platform"`
		OTP               string   `json:"otp"`
		Go                string   `json:"go"`
		Schedulers        int      `json:"schedulers"`
		SamplesPerVariant int      `json:"samples_per_variant"`
		Samples           []sample `json:"samples"`
	}{time.Now().UTC().Format(time.RFC3339), runtime.GOOS + "/" + runtime.GOARCH, otp, runtime.Version(), 1, *samples, results}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(*output, append(data, '\n'), 0644); err != nil {
		return err
	}
	fmt.Println("\nWorkload     Variant       Median µs   Managed allocations   Peak cells")
	for _, workload := range workloads {
		for _, variant := range []string{"cells", "optimized", "erlang"} {
			var times []int
			var representative sample
			for _, result := range results {
				if result.Workload == workload && result.Variant == variant {
					times = append(times, result.Microseconds)
					representative = result
				}
			}
			sort.Ints(times)
			fmt.Printf("%-12s %-12s %10d %21d %12d\n", workload, variant, times[len(times)/2], representative.Allocations, representative.PeakCells)
		}
	}
	fmt.Println("\nRaw measurements:", *output)
	return nil
}

func measurementScript(workload string, samples int) string {
	return fmt.Sprintf(`
try
 io:format("OTP:~s~n", [erlang:system_info(otp_release)]),
 Variants = [{optimized, fun bench_%[1]s_optimized:main/0}, {cells, fun bench_%[1]s_cells:main/0}, {erlang, fun bench_reference:%[1]s/0}],
 Measure = fun({Name, Run}, Print) ->
  Parent = self(),
  {Pid, Ref} = spawn_monitor(fun() ->
   erlang:garbage_collect(),
   {reductions, Before} = process_info(self(), reductions),
   {Micros, ok} = timer:tc(Run),
   {reductions, After} = process_info(self(), reductions),
   Parent ! {self(), Micros, linglang_rt:stats(), After - Before}
  end),
  receive
   {Pid, Micros, Stats, Reductions} ->
    receive {'DOWN', Ref, process, Pid, normal} -> ok end,
    case Print of
     false -> ok;
     true -> io:format("{\"workload\":\"%[1]s\",\"variant\":\"~s\",\"microseconds\":~p,\"managed_allocations\":~p,\"peak_managed_cells\":~p,\"reductions\":~p}~n", [Name, Micros, maps:get(allocated_cells, Stats), maps:get(peak_live_cells, Stats), Reductions])
    end;
   {'DOWN', Ref, process, Pid, Reason} -> error({benchmark_failed, Name, Reason})
  after 30000 -> error(benchmark_timeout)
  end
 end,
 lists:foreach(fun(_) -> lists:foreach(fun(V) -> Measure(V, false) end, Variants) end, [1,2]),
 lists:foreach(fun(Round) ->
  {A, B} = lists:split(Round rem 3, Variants),
  lists:foreach(fun(V) -> Measure(V, true) end, B ++ A)
 end, lists:seq(1, %[2]d)),
 halt(0)
catch Class:Reason:Stack -> io:format("~p:~p~n~p~n", [Class, Reason, Stack]), halt(1)
end.`, workload, samples)
}
