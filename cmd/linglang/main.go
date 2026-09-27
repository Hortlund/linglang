package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"linglang/internal/compiler"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "linglang:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Println("Usage:\n  linglang run [--no-opt] [--gc-stats] [--gc-stress] <file.lang>\n  linglang build [--no-opt] [-o directory] <file.lang>\n  linglang emit [--no-opt] <file.lang>")
		return nil
	}
	command := args[0]
	if command != "run" && command != "build" && command != "emit" {
		return fmt.Errorf("unknown command %q (try linglang help)", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	output := "_build"
	gcStats, gcStress := false, false
	noOpt := flags.Bool("no-opt", false, "use the original cell-based lowering for comparisons")
	if command == "build" {
		flags.StringVar(&output, "o", output, "build directory")
	}
	if command == "run" {
		flags.BoolVar(&gcStats, "gc-stats", false, "print managed-heap statistics to stderr")
		flags.BoolVar(&gcStress, "gc-stress", false, "collect at every safe point (debugging; slower)")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("%s expects one source file", command)
	}
	source, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}
	program, err := compiler.CompileWithOptions(flags.Arg(0), source, compiler.Options{DisableOptimizations: *noOpt})
	if err != nil {
		return err
	}
	if command == "emit" {
		fmt.Print(program)
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
	cmd := exec.Command("erl", "-noshell", "-pa", dir, "-eval", evaluationScript(gcStress, gcStats))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("BEAM execution failed: %w", err)
	}
	return nil
}

func evaluationScript(gcStress, gcStats bool) string {
	// Run language main in a monitored process. An OTP supervisor can terminate
	// its owner via a link; try/catch alone cannot catch that exit signal.
	script := fmt.Sprintf("Runner = fun() -> linglang_rt:set_gc_stress(%t), ", gcStress)
	script += "ExitCode = try linglang_program:main() of _ -> 0 catch Class:Reason:Stack -> io:format(standard_error, \"linglang runtime error: ~p:~p~n~p~n\", [Class, Reason, Stack]), 1 end, "
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
	if _, err := exec.LookPath("erlc"); err != nil {
		return fmt.Errorf("Erlang/OTP is required: erlc is not on PATH")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	files := compiler.RuntimeSources()
	files["linglang_program.erl"] = program
	args := []string{"-o", dir}
	for name, source := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
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
