package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"linglang/internal/compiler"
)

var frontendPhases = []string{"lex", "parse", "resolve", "check", "emit"}

// Inputs and prerequisite passes are prepared outside timing. The fixed lexer
// corpus exercises real compiler source; emission uses the executable subset.
func runFrontendBench(samples, warmup int, root, buildDir, output string, log io.Writer) (*report, error) {
	if samples < 1 || samples > 100 || warmup < 0 || warmup > 100 {
		return nil, fmt.Errorf("samples must be 1–100 and warmup must be 0–100")
	}
	library, err := frontendSources(root, "bootstrap/emitter")
	if err != nil {
		return nil, err
	}
	var sources []compiler.SourceFile
	for _, source := range library {
		if filepath.Base(source.Filename) != "main.lang" {
			sources = append(sources, source)
		}
	}
	sources = append(sources, compiler.SourceFile{Filename: "benchmark_driver.lang", Source: []byte(frontendDriver)})
	r := &report{Timestamp: time.Now().UTC().Format(time.RFC3339), Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Go: runtime.Version(), Variants: []string{"optimized", "cells"}, Schedulers: 1,
		SamplesPerVariant: samples, WarmupPerVariant: warmup, Corpora: make(map[string][]string)}
	fingerprint := sha256.New()
	for _, source := range sources {
		fmt.Fprintf(fingerprint, "%s\x00%s\x00", source.Filename, source.Source)
	}
	dir, err := filepath.Abs(buildDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	generated := compiler.RuntimeSources()
	for _, variant := range r.Variants {
		module, err := compiler.CompileFilesWithOptions(sources, compiler.Options{DisableOptimizations: variant == "cells"})
		if err != nil {
			return nil, err
		}
		module = strings.Replace(module, "-module(linglang_program).", "-module(frontend_"+variant+").", 1)
		module = strings.Replace(module, "-export([main/0]).", fmt.Sprintf("-export([main/0, f_%x/1, f_%x/2]).", "benchmarkPrepare", "benchmarkRun"), 1)
		generated["frontend_"+variant+".erl"] = module
	}
	var names []string
	for name := range generated {
		names = append(names, name)
	}
	sort.Strings(names)
	args := []string{"-o", dir}
	for _, name := range names {
		fmt.Fprintf(fingerprint, "%s\x00%s\x00", name, generated[name])
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(generated[name]), 0644); err != nil {
			return nil, err
		}
		args = append(args, path)
	}
	if _, err := execute(2*time.Minute, "erlc", args...); err != nil {
		return nil, err
	}
	for _, phase := range frontendPhases {
		corpus := "bootstrap/lexer"
		if phase == "emit" {
			corpus = "examples/bootstrap_demo"
		}
		files, err := frontendSources(root, corpus)
		if err != nil {
			return nil, err
		}
		var paths []string
		for _, source := range files {
			fmt.Fprintf(fingerprint, "%s\x00%s\x00", source.Filename, source.Source)
			r.Corpora[phase] = append(r.Corpora[phase], source.Filename)
			path, err := filepath.Abs(filepath.Join(root, source.Filename))
			if err != nil {
				return nil, err
			}
			paths = append(paths, path)
		}
		fmt.Fprintln(log, "Measuring bootstrap", phase)
		script := frontendMeasurementScript(phase, samples, warmup)
		budget := time.Duration((samples+warmup)*2)*2*time.Minute + 10*time.Second
		data, err := execute(budget, "erl", append([]string{"+S", "1", "-noshell", "-pa", dir, "-eval", script, "-extra"}, paths...)...)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.HasPrefix(line, "OTP:") {
				r.OTP = strings.TrimPrefix(line, "OTP:")
				continue
			}
			if strings.HasPrefix(line, "VM:") {
				r.VM = strings.TrimPrefix(line, "VM:")
				continue
			}
			var s sample
			if err := json.Unmarshal([]byte(line), &s); err != nil {
				return nil, fmt.Errorf("invalid phase measurement %q: %w", line, err)
			}
			r.Samples = append(r.Samples, s)
		}
	}
	r.SourceSHA256 = fmt.Sprintf("%x", fingerprint.Sum(nil))
	fmt.Fprintln(log, "\nPhase       Backend       Median µs    P95 µs   Managed allocs   Peak cells   Reductions   Cell GCs")
	for _, phase := range frontendPhases {
		expected := 0
		for _, variant := range r.Variants {
			var group []sample
			for _, s := range r.Samples {
				if s.Workload == phase && s.Variant == variant {
					group = append(group, s)
				}
			}
			if len(group) != samples {
				return nil, fmt.Errorf("%s/%s: got %d samples, want %d", phase, variant, len(group), samples)
			}
			for _, measured := range group {
				if measured.ResultCount == nil || *measured.ResultCount <= 0 {
					return nil, fmt.Errorf("%s/%s: missing phase result", phase, variant)
				}
				if expected == 0 {
					expected = *measured.ResultCount
				} else if expected != *measured.ResultCount {
					return nil, fmt.Errorf("%s: inconsistent phase results", phase)
				}
			}
			s := summarize(group)
			r.Summaries = append(r.Summaries, s)
			near := group[len(group)/2]
			fmt.Fprintf(log, "%-11s %-11s %10.1f %9d %16d %12d %12d %10d\n", phase, variant, s.MedianMicros, s.P95Micros, near.Allocations, near.PeakCells, near.Reductions, near.Collections)
		}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(output, append(data, '\n'), 0644); err != nil {
		return nil, err
	}
	fmt.Fprintln(log, "\nRaw measurements:", output)
	return r, nil
}

func frontendSources(root, directory string) ([]compiler.SourceFile, error) {
	entries, err := os.ReadDir(filepath.Join(root, directory))
	if err != nil {
		return nil, err
	}
	var sources []compiler.SourceFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lang") || strings.HasSuffix(entry.Name(), "_test.lang") {
			continue
		}
		name := filepath.Join(directory, entry.Name())
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		sources = append(sources, compiler.SourceFile{Filename: name, Source: data})
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("empty corpus %s", directory)
	}
	return sources, nil
}

const frontendDriver = `package main
type BenchmarkInput struct { filename string; text string; tree SyntaxNode }
func benchmarkPrepare(phase string) List[BenchmarkInput] {
 var reversed List[BenchmarkInput]
 for _, filename := range args() {
  source := readFile(filename); assert(source.ok)
  input := BenchmarkInput{filename: filename, text: source.value}
  if phase != "lex" && phase != "parse" { parsed := parse(filename, source.value); assert(parsed.ok); input.tree = parsed.root }
  reversed = prepend(input, reversed)
 }
 var inputs List[BenchmarkInput]
 for _, input := range reversed { inputs = prepend(input, inputs) }
 return inputs
}
func benchmarkRun(phase string, inputs List[BenchmarkInput]) int {
 var reversed List[SyntaxNode]
 count := 0
 for _, input := range inputs {
  if phase == "lex" { result := lex(input.filename, input.text); assert(result.ok); count += len(result.tokens)
  } else if phase == "parse" { result := parse(input.filename, input.text); assert(result.ok); count += len(result.root.children)
  } else { reversed = prepend(input.tree, reversed) }
 }
 files := syntaxOrder(reversed)
 if phase == "resolve" { result := resolve(files); assert(result.ok); count = len(result.references)
 } else if phase == "check" { result := checkPackageInfo(files, true); assert(result.ok); count = len(result.expressions)
 } else if phase == "emit" { result := emitPackage(files); assert(result.ok); count = len(result.module) }
 assert(count > 0)
 return count
}
func main() {}
`

func frontendMeasurementScript(phase string, samples, warmup int) string {
	return fmt.Sprintf(`
try
 io:format("OTP:~s~n", [erlang:system_info(otp_release)]),
 io:format("VM:~s~n", [string:trim(erlang:system_info(system_version))]),
 Variants = [{optimized, frontend_optimized}, {cells, frontend_cells}],
 Measure = fun({Name, Module}, Print) ->
  Parent = self(),
  {Pid, Ref} = spawn_monitor(fun() ->
   linglang_rt:set_arguments([unicode:characters_to_binary(P) || P <- init:get_plain_arguments()]),
   Input = Module:f_%[4]x(<<"%[1]s">>),
   linglang_rt:collect(),
   BeforeStats = linglang_rt:stats(),
   %% Reset only the peak counter: preparation has no retained managed cells.
   #{live_cells := 0, root_frames := 0, root_entries := 0} = BeforeStats,
   put({linglang_gc, peak_live_cells}, 0),
   erlang:garbage_collect(),
   {reductions, Before} = process_info(self(), reductions),
   {Micros, Count} = timer:tc(fun() -> Module:f_%[5]x(<<"%[1]s">>, Input) end),
   {reductions, After} = process_info(self(), reductions),
   true = is_integer(Count) andalso Count > 0,
   RawStats = linglang_rt:stats(),
   Stats = RawStats#{allocated_cells := maps:get(allocated_cells, RawStats) - maps:get(allocated_cells, BeforeStats),
                    collections := maps:get(collections, RawStats) - maps:get(collections, BeforeStats)},
   linglang_rt:collect(),
   #{live_cells := 0, root_frames := 0, root_entries := 0} = linglang_rt:stats(),
   Parent ! {self(), Micros, Stats, After - Before, Count}
  end),
  receive
   {Pid, Micros, Stats, Reductions, Count} ->
    receive {'DOWN', Ref, process, Pid, normal} -> ok end,
    case Print of
     false -> ok;
     true -> io:format("{\"workload\":\"%[1]s\",\"variant\":\"~s\",\"microseconds\":~p,\"managed_allocations\":~p,\"peak_managed_cells\":~p,\"reductions\":~p,\"managed_collections\":~p,\"result_count\":~p}~n", [Name, Micros, maps:get(allocated_cells, Stats), maps:get(peak_live_cells, Stats), Reductions, maps:get(collections, Stats), Count])
    end;
   {'DOWN', Ref, process, Pid, Reason} -> error({benchmark_failed, Name, Reason})
  after 120000 -> error(benchmark_timeout)
  end
 end,
 lists:foreach(fun(_) -> lists:foreach(fun(V) -> Measure(V, false) end, Variants) end, lists:seq(1, %[3]d)),
 lists:foreach(fun(Round) ->
  {A, B} = lists:split(Round rem 2, Variants),
  lists:foreach(fun(V) -> Measure(V, true) end, B ++ A)
 end, lists:seq(1, %[2]d)),
 halt(0)
catch Class:Reason:Stack -> io:format("~p:~p~n~p~n", [Class, Reason, Stack]), halt(1)
end.`, phase, samples, warmup, "benchmarkPrepare", "benchmarkRun")
}
