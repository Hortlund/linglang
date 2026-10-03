// bench compares linglang backends and native Erlang, Elixir, and Go references.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	Workload         string  `json:"workload"`
	Variant          string  `json:"variant"`
	Microseconds     int     `json:"microseconds"`
	Allocations      int     `json:"managed_allocations"`
	PeakCells        int     `json:"peak_managed_cells"`
	Reductions       int     `json:"reductions"`
	Collections      int     `json:"managed_collections"`
	GoAllocations    *uint64 `json:"go_allocations,omitempty"`
	GoAllocatedBytes *uint64 `json:"go_allocated_bytes,omitempty"`
}

var workloads = []string{"arithmetic", "calls", "structs", "pointers", "lists", "maps", "strings", "messages"}
var variants = []string{"cells", "optimized", "erlang"}
var comparisonVariants = []string{"optimized", "erlang", "elixir", "go"}

type config struct {
	samples, warmup int
	workloads       []string
	sourceDir, dir  string
	output          string
	compare         bool
}

type summary struct {
	Workload     string  `json:"workload"`
	Variant      string  `json:"variant"`
	MedianMicros float64 `json:"median_microseconds"`
	P95Micros    int     `json:"p95_microseconds"`
}

type report struct {
	Timestamp         string    `json:"timestamp"`
	Platform          string    `json:"platform"`
	OTP               string    `json:"otp"`
	VM                string    `json:"vm"`
	Go                string    `json:"go"`
	NativeGo          string    `json:"native_go,omitempty"`
	Elixir            string    `json:"elixir,omitempty"`
	Variants          []string  `json:"variants"`
	Schedulers        int       `json:"schedulers"`
	GoMaxProcs        int       `json:"go_max_procs,omitempty"`
	SamplesPerVariant int       `json:"samples_per_variant"`
	WarmupPerVariant  int       `json:"warmup_per_variant"`
	SourceSHA256      string    `json:"source_sha256"`
	Samples           []sample  `json:"samples"`
	Summaries         []summary `json:"summaries"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	samples := flag.Int("samples", 7, "measured runs per workload and variant")
	warmup := flag.Int("warmup", 2, "unmeasured warmup runs per workload and variant")
	workload := flag.String("workload", "all", "one workload: "+strings.Join(workloads, ", ")+", or all")
	output := flag.String("out", "_build/benchmarks/results.json", "JSON result file")
	compare := flag.Bool("compare", false, "compare optimized linglang, Erlang, Elixir, and Go (requires elixir)")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flag.Args(), " "))
	}
	selected := workloads
	if *workload != "all" {
		selected = []string{*workload}
	}
	_, err := runBench(config{
		samples: *samples, warmup: *warmup, workloads: selected,
		sourceDir: "benchmarks", dir: "_build/benchmarks", output: *output,
		compare: *compare,
	}, os.Stdout)
	return err
}

func runBench(cfg config, log io.Writer) (*report, error) {
	if cfg.samples < 1 || cfg.samples > 100 {
		return nil, fmt.Errorf("samples must be between 1 and 100")
	}
	if cfg.warmup < 0 || cfg.warmup > 100 {
		return nil, fmt.Errorf("warmup must be between 0 and 100")
	}
	if len(cfg.workloads) == 0 {
		return nil, fmt.Errorf("select at least one workload")
	}
	seen := make(map[string]bool)
	for _, workload := range cfg.workloads {
		valid := false
		for _, known := range workloads {
			valid = valid || workload == known
		}
		if !valid || seen[workload] {
			return nil, fmt.Errorf("unknown or duplicate workload %q", workload)
		}
		seen[workload] = true
	}
	selectedVariants := variants
	if cfg.compare {
		selectedVariants = comparisonVariants
		for _, executable := range []string{"go", "elixir"} {
			if _, err := exec.LookPath(executable); err != nil {
				return nil, fmt.Errorf("-compare requires %s: %w", executable, err)
			}
		}
	}
	dir, err := filepath.Abs(cfg.dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	sources := compiler.RuntimeSources()
	reference, err := os.ReadFile(filepath.Join(cfg.sourceDir, "bench_reference.erl"))
	if err != nil {
		return nil, fmt.Errorf("run this command from the repository root: %w", err)
	}
	sources["bench_reference.erl"] = string(reference)
	for _, name := range cfg.workloads {
		path := filepath.Join(cfg.sourceDir, name+".lang")
		source, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		for _, variant := range []string{"optimized", "cells"} {
			if cfg.compare && variant == "cells" {
				continue
			}
			program, err := compiler.CompileWithOptions(path, source, compiler.Options{DisableOptimizations: variant == "cells"})
			if err != nil {
				return nil, err
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
	fingerprint := sha256.New()
	for _, name := range names {
		fmt.Fprintf(fingerprint, "%s\x00%s\x00", name, sources[name])
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(sources[name]), 0644); err != nil {
			return nil, err
		}
		args = append(args, path)
	}
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	buildOutput, buildErr := exec.CommandContext(buildCtx, "erlc", args...).CombinedOutput()
	buildCancel()
	if buildErr != nil {
		return nil, fmt.Errorf("erlc: %w\n%s", buildErr, buildOutput)
	}
	if cfg.compare {
		for _, name := range []string{"bench_reference.ex", "go/main.go"} {
			data, err := os.ReadFile(filepath.Join(cfg.sourceDir, name))
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(fingerprint, "%s\x00%s\x00", name, data)
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), data, 0644); err != nil {
				return nil, err
			}
		}
		if _, err := execute(2*time.Minute, "go", "build", "-o", filepath.Join(dir, "bench_go"), filepath.Join(dir, "main.go")); err != nil {
			return nil, err
		}
		// Compile before timing; launch through Elixir later to load its standard
		// library on the same OTP VM as the other BEAM implementations.
		compile := `args = System.argv(); Code.compile_file(hd(args)) |> Enum.each(fn {module, binary} -> File.write!(Path.join(Enum.at(args, 1), Atom.to_string(module) <> ".beam"), binary) end)`
		if _, err := execute(2*time.Minute, "elixir", "--erl", "+S 1", "-e", compile, filepath.Join(dir, "bench_reference.ex"), dir); err != nil {
			return nil, err
		}
	}

	var results []sample
	otp := ""
	vm := ""
	nativeGo := ""
	elixir := ""
	for _, workload := range cfg.workloads {
		fmt.Fprintln(log, "Measuring", workload)
		// Allow every requested run its 30-second deadline, plus VM startup.
		budget := time.Duration((cfg.samples+cfg.warmup)*len(selectedVariants))*30*time.Second + 10*time.Second
		script := measurementScript(workload, cfg.samples, cfg.warmup, cfg.compare)
		var data []byte
		if cfg.compare {
			// Parse the shared Erlang measurement script; argument passing avoids
			// interpolating user paths or source into shell/Elixir expressions.
			eval := `IO.puts("Elixir:" <> System.version()); {:ok, tokens, _} = :erl_scan.string(String.to_charlist(hd(System.argv()))); {:ok, forms} = :erl_parse.parse_exprs(tokens); :erl_eval.exprs(forms, [])`
			data, err = execute(budget, "elixir", "--erl", "+S 1", "-pa", dir, "-e", eval, script)
		} else {
			data, err = execute(budget, "erl", "+S", "1", "-noshell", "-pa", dir, "-eval", script)
		}
		if err != nil {
			return nil, err
		}
		if cfg.compare {
			goData, err := execute(budget, filepath.Join(dir, "bench_go"), "-workload", workload, "-samples", fmt.Sprint(cfg.samples), "-warmup", fmt.Sprint(cfg.warmup))
			if err != nil {
				return nil, err
			}
			data = append(data, goData...)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.HasPrefix(line, "OTP:") {
				otp = strings.TrimPrefix(line, "OTP:")
				continue
			}
			if strings.HasPrefix(line, "VM:") {
				vm = strings.TrimPrefix(line, "VM:")
				continue
			}
			if strings.HasPrefix(line, "Elixir:") {
				elixir = strings.TrimSpace(strings.TrimPrefix(line, "Elixir:"))
				continue
			}
			if strings.HasPrefix(line, "Go:") {
				nativeGo = strings.TrimSpace(strings.TrimPrefix(line, "Go:"))
				continue
			}
			var result sample
			if err := json.Unmarshal([]byte(line), &result); err != nil {
				return nil, fmt.Errorf("invalid benchmark output %q: %w", line, err)
			}
			if result.Variant == "go" && (result.GoAllocations == nil || result.GoAllocatedBytes == nil) {
				return nil, fmt.Errorf("Go sample has no allocation measurements: %q", line)
			}
			results = append(results, result)
		}
	}

	report := &report{
		Timestamp: time.Now().UTC().Format(time.RFC3339), Platform: runtime.GOOS + "/" + runtime.GOARCH,
		OTP: otp, VM: vm, Go: runtime.Version(), Schedulers: 1, SamplesPerVariant: cfg.samples,
		NativeGo: nativeGo, Elixir: elixir, Variants: selectedVariants,
		WarmupPerVariant: cfg.warmup, SourceSHA256: fmt.Sprintf("%x", fingerprint.Sum(nil)), Samples: results,
	}
	if cfg.compare {
		report.GoMaxProcs = 1
	}
	fmt.Fprintln(log, "\nWorkload     Variant       Median µs     P95 µs   Managed allocs   Peak cells   Reductions   Cell GCs   Go allocs")
	for _, workload := range cfg.workloads {
		for _, variant := range selectedVariants {
			var group []sample
			for _, result := range results {
				if result.Workload == workload && result.Variant == variant {
					group = append(group, result)
				}
			}
			if len(group) != cfg.samples {
				return nil, fmt.Errorf("%s/%s: got %d samples, want %d", workload, variant, len(group), cfg.samples)
			}
			summary := summarize(group)
			report.Summaries = append(report.Summaries, summary)
			// Counters come from the run nearest the median elapsed time.
			representative := group[len(group)/2]
			if variant == "go" {
				fmt.Fprintf(log, "%-12s %-12s %10.1f %10d %16s %12s %12s %10s %10d\n", workload, variant,
					summary.MedianMicros, summary.P95Micros, "-", "-", "-", "-", *representative.GoAllocations)
			} else {
				fmt.Fprintf(log, "%-12s %-12s %10.1f %10d %16d %12d %12d %10d %10s\n", workload, variant,
					summary.MedianMicros, summary.P95Micros, representative.Allocations,
					representative.PeakCells, representative.Reductions, representative.Collections, "-")
			}
		}
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.output), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(cfg.output, append(data, '\n'), 0644); err != nil {
		return nil, err
	}
	fmt.Fprintln(log, "\nRaw measurements:", cfg.output)
	return report, nil
}

func execute(budget time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	data, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n%s", filepath.Base(name), err, data)
	}
	return data, nil
}

func summarize(group []sample) summary {
	sort.Slice(group, func(i, j int) bool { return group[i].Microseconds < group[j].Microseconds })
	n := len(group)
	median := float64(group[n/2].Microseconds)
	if n%2 == 0 {
		median = (float64(group[n/2-1].Microseconds) + median) / 2
	}
	return summary{group[0].Workload, group[0].Variant, median, group[(95*n+99)/100-1].Microseconds}
}

func measurementScript(workload string, samples, warmup int, compare bool) string {
	entries := fmt.Sprintf("[{optimized, fun bench_%s_optimized:main/0}, {cells, fun bench_%s_cells:main/0}, {erlang, fun bench_reference:%s/0}]", workload, workload, workload)
	if compare {
		entries = fmt.Sprintf("[{optimized, fun bench_%s_optimized:main/0}, {erlang, fun bench_reference:%s/0}, {elixir, fun 'Elixir.BenchReference':%s/0}]", workload, workload, workload)
	}
	return fmt.Sprintf(`
try
 io:format("OTP:~s~n", [erlang:system_info(otp_release)]),
 io:format("VM:~s~n", [string:trim(erlang:system_info(system_version))]),
 Variants = %[4]s,
 Measure = fun({Name, Run}, Print) ->
  Parent = self(),
  {Pid, Ref} = spawn_monitor(fun() ->
   erlang:garbage_collect(),
   {reductions, Before} = process_info(self(), reductions),
   {Micros, ok} = timer:tc(Run),
   {reductions, After} = process_info(self(), reductions),
   Stats = linglang_rt:stats(),
   %% Validate root cleanup outside the timing/counter interval.
   linglang_rt:collect(),
   #{live_cells := 0, root_frames := 0, root_entries := 0} = linglang_rt:stats(),
   Parent ! {self(), Micros, Stats, After - Before}
  end),
  receive
   {Pid, Micros, Stats, Reductions} ->
    receive {'DOWN', Ref, process, Pid, normal} -> ok end,
    case Print of
     false -> ok;
     true -> io:format("{\"workload\":\"%[1]s\",\"variant\":\"~s\",\"microseconds\":~p,\"managed_allocations\":~p,\"peak_managed_cells\":~p,\"reductions\":~p,\"managed_collections\":~p}~n", [Name, Micros, maps:get(allocated_cells, Stats), maps:get(peak_live_cells, Stats), Reductions, maps:get(collections, Stats)])
    end;
   {'DOWN', Ref, process, Pid, Reason} -> error({benchmark_failed, Name, Reason})
  after 30000 -> error(benchmark_timeout)
  end
 end,
 lists:foreach(fun(_) -> lists:foreach(fun(V) -> Measure(V, false) end, Variants) end, lists:seq(1, %[3]d)),
 lists:foreach(fun(Round) ->
  {A, B} = lists:split(Round rem 3, Variants),
  lists:foreach(fun(V) -> Measure(V, true) end, B ++ A)
 end, lists:seq(1, %[2]d)),
 halt(0)
catch Class:Reason:Stack -> io:format("~p:~p~n~p~n", [Class, Reason, Stack]), halt(1)
end.`, workload, samples, warmup, entries)
}
