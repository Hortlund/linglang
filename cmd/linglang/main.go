package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"linglang/internal/compiler"
	"linglang/internal/lsp"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		var reported reportedError
		if !errors.As(err, &reported) {
			fmt.Fprintln(os.Stderr, "linglang:", err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(commandUsage())
		return nil
	}
	command := args[0]
	if command == "check" {
		return checkCommand(args[1:], os.Stdout, os.Stderr)
	}
	if command == "describe" {
		return describeCommand(args[1:], os.Stdout, os.Stderr)
	}
	if command == "deps" {
		return depsCommand(args[1:])
	}
	if command == "init" {
		return initCommand(args[1:])
	}
	if command == "lsp" {
		if len(args) != 1 {
			return fmt.Errorf("lsp accepts no arguments")
		}
		return lsp.NewServer().Serve(os.Stdin, os.Stdout)
	}
	if command == "fmt" {
		return formatCommand(args[1:])
	}
	if command == "test" {
		return testCommand(args[1:])
	}
	if command != "run" && command != "build" && command != "pack" && command != "release" && command != "emit" {
		return fmt.Errorf("unknown command %q (try linglang help)", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	output := "_build"
	gcStats, gcStress := false, false
	noOpt := flags.Bool("no-opt", false, "use the original cell-based lowering for comparisons")
	if command == "build" {
		flags.StringVar(&output, "o", output, "build directory")
	}
	if command == "pack" {
		output = ""
		flags.StringVar(&output, "o", output, "output executable (default: <source-name>.escript)")
	}
	if command == "release" {
		output = ""
		flags.StringVar(&output, "o", output, "output archive (default: <source-name>-<os>-<arch>.tar.gz)")
	}
	if command == "run" || command == "pack" || command == "release" {
		flags.BoolVar(&gcStats, "gc-stats", false, "print managed-heap statistics to stderr")
		flags.BoolVar(&gcStress, "gc-stress", false, "collect at every safe point (debugging; slower)")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() == 0 || (command != "run" && flags.NArg() != 1) {
		return fmt.Errorf("%s expects one source file or directory (only run accepts program arguments)", command)
	}
	sources, err := readSources(flags.Arg(0))
	if err != nil {
		return err
	}
	program, err := compiler.CompileFilesWithOptions(sources, compiler.Options{DisableOptimizations: *noOpt})
	if err != nil {
		return err
	}
	if command == "emit" {
		fmt.Print(program)
		return nil
	}
	if command == "pack" || command == "release" {
		if output == "" {
			absolute, err := filepath.Abs(flags.Arg(0))
			if err != nil {
				return err
			}
			name := filepath.Base(absolute)
			if info, err := os.Stat(flags.Arg(0)); err == nil && !info.IsDir() {
				name = name[:len(name)-len(filepath.Ext(name))]
			}
			output = name + ".escript"
			if command == "release" {
				output = name + "-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
			}
		}
		inputs, err := compiler.InputFiles(sources)
		if err != nil {
			return err
		}
		if err := protectSources(output, inputs); err != nil {
			return err
		}
		if command == "release" {
			err = release(output, program, gcStress, gcStats)
		} else {
			err = pack(output, program, gcStress, gcStats)
		}
		if err != nil {
			return err
		}
		absolute, err := filepath.Abs(output)
		if err != nil {
			return err
		}
		if command == "release" {
			fmt.Println("Built runtime release in", absolute)
		} else {
			fmt.Println("Packed executable in", absolute)
		}
		return nil
	}
	dir := output
	if command == "run" {
		dir, err = os.MkdirTemp("", "linglang-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
	}
	if err := build(dir, program); err != nil {
		return err
	}
	if command == "build" {
		absolute, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		fmt.Println("Built BEAM modules in", absolute)
		return nil
	}
	erlArgs := []string{"-noshell", "-pa", dir, "-eval", evaluationScript(gcStress, gcStats), "-extra"}
	erlArgs = append(erlArgs, flags.Args()[1:]...)
	cmd := exec.Command("erl", erlArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("BEAM execution failed: %w", err)
	}
	return nil
}

// A directory is one package: only its immediate .lang files are loaded.
// ReadDir returns filename order; auxiliary files and subdirectories are ignored.
// Explicit paths may be streams such as /dev/stdin or named pipes.
func readSources(path string) ([]compiler.SourceFile, error) {
	return readPackageSources(path, false)
}

func readPackageSources(path string, includeTests bool) ([]compiler.SourceFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	paths := []string{path}
	directory := info.IsDir()
	if directory {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		paths = nil
		for _, entry := range entries {
			if !includeTests && strings.HasSuffix(entry.Name(), "_test.lang") {
				continue
			}
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".lang" {
				paths = append(paths, filepath.Join(path, entry.Name()))
			}
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("%s: directory contains no .lang source files", path)
		}
	}
	var sources []compiler.SourceFile
	for _, filename := range paths {
		info, err := os.Stat(filename)
		if err != nil {
			return nil, err
		}
		if directory && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s: source must be a regular file", filename)
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, err
		}
		sources = append(sources, compiler.SourceFile{Filename: filename, Source: data})
	}
	return sources, nil
}

func evaluationScript(gcStress, gcStats bool) string {
	return evaluationScriptFor("linglang_program:main()", gcStress, gcStats)
}

func evaluationScriptFor(entry string, gcStress, gcStats bool) string {
	// Run language main in a monitored process. An OTP supervisor can terminate
	// its owner via a link; try/catch alone cannot catch that exit signal.
	script := fmt.Sprintf("Runner = fun() -> linglang_rt:set_gc_stress(%t), ", gcStress)
	script += "ExitCode = try " + entry + " of _ -> 0 catch error:linglang_reported_failure -> 1; error:{linglang_assertion, File, Line} -> io:format(standard_error, \"~ts:~p: assertion failed~n\", [File, Line]), 1; Class:Reason:Stack -> io:format(standard_error, \"linglang runtime error: ~p:~p~n~p~n\", [Class, Reason, Stack]), 1 end, "
	script += "exit({linglang_completed, ExitCode, linglang_rt:stats()}) end, "
	script += "{Pid, Ref} = spawn_monitor(Runner), receive {'DOWN', Ref, process, Pid, {linglang_completed, Code, Stats}} -> "
	if gcStats {
		script += "io:format(standard_error, \"linglang GC: ~p~n\", [Stats]), "
	}
	script += "halt(Code); {'DOWN', Ref, process, Pid, Failure} -> io:format(standard_error, \"linglang process exited: ~tp~n\", [Failure]), "
	if gcStats {
		script += "io:format(standard_error, \"linglang GC: unavailable after process exit~n\", []), "
	}
	return script + "halt(1) end."
}

func build(dir, program string) error {
	files := compiler.RuntimeSources()
	files["linglang_program.erl"] = program
	return buildSources(dir, files)
}

func buildSources(dir string, files map[string]string) error {
	if _, err := exec.LookPath("erlc"); err != nil {
		return fmt.Errorf("Erlang/OTP is required: erlc is not on PATH")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	args := []string{"-o", dir}
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(files[name]), 0644); err != nil {
			return err
		}
		args = append(args, path)
	}
	cmd := exec.Command("erlc", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Erlang compilation failed: %w\n%s", err, output)
	}
	return nil
}
