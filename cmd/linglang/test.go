package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"linglang/internal/compiler"
)

func testCommand(args []string) error {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	noOpt := flags.Bool("no-opt", false, "use the original compiler backend")
	gcStress := flags.Bool("gc-stress", false, "collect at every safe point")
	gcStats := flags.Bool("gc-stats", false, "print each test's managed-heap statistics")
	timeout := flags.Duration("timeout", 30*time.Second, "maximum time per test, including VM startup and shutdown")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("test expects one source file or directory")
	}
	if *timeout <= 0 {
		return fmt.Errorf("test timeout must be positive")
	}
	path := "."
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	sources, err := readPackageSources(path, true)
	if err != nil {
		return err
	}
	program, tests, err := compiler.CompileTestFilesWithOptions(sources, compiler.Options{DisableOptimizations: *noOpt})
	if err != nil {
		return err
	}
	if len(tests) == 0 {
		return fmt.Errorf("%s: no Test functions found in _test.lang files", path)
	}
	if _, err := exec.LookPath("erl"); err != nil {
		return fmt.Errorf("Erlang/OTP is required: erl is not on PATH")
	}
	dir, err := os.MkdirTemp("", "linglang-test-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := build(dir, program); err != nil {
		return err
	}
	failures := 0
	for _, test := range tests {
		fmt.Println("RUN", test.Name)
		// A fresh VM isolates messages, persistent runtime arguments, supervisors,
		// and unmonitored workers. A timeout kills that VM and all its BEAM processes.
		var bytes []string
		for _, b := range []byte(test.Name) {
			bytes = append(bytes, strconv.Itoa(int(b)))
		}
		entry := "linglang_program:run_test(list_to_binary([" + strings.Join(bytes, ",") + "]))"
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		cmd := exec.CommandContext(ctx, "erl", "-noshell", "-pa", dir, "-eval", evaluationScriptFor(entry, *gcStress, *gcStats))
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		err := cmd.Run()
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		cancel()
		if err == nil {
			fmt.Println("PASS", test.Name)
			continue
		}
		failures++
		if timedOut {
			fmt.Printf("FAIL %s (%s:%d): timeout after %s\n", test.Name, test.Filename, test.Line, *timeout)
		} else {
			fmt.Printf("FAIL %s (%s:%d): %v\n", test.Name, test.Filename, test.Line, err)
		}
	}
	fmt.Printf("Tests: %d passed, %d failed\n", len(tests)-failures, failures)
	if failures > 0 {
		return fmt.Errorf("test suite failed")
	}
	return nil
}
