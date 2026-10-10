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
	structured := flags.Bool("json", false, "write versioned JSON Lines events; send test output to stderr")
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
		return testFailure(err, compiler.CodeInput, *structured)
	}
	program, tests, err := compiler.CompileTestFilesWithOptions(sources, compiler.Options{DisableOptimizations: *noOpt})
	if err != nil {
		return testFailure(err, compiler.CodeInternal, *structured)
	}
	if len(tests) == 0 {
		return testFailure(fmt.Errorf("%s: no Test functions found in _test.lang files", path), compiler.CodeTest, *structured)
	}
	if _, err := exec.LookPath("erl"); err != nil {
		return testFailure(fmt.Errorf("Erlang/OTP is required: erl is not on PATH"), compiler.CodeTest, *structured)
	}
	dir, err := os.MkdirTemp("", "linglang-test-*")
	if err != nil {
		return testFailure(err, compiler.CodeTest, *structured)
	}
	defer os.RemoveAll(dir)
	if err := build(dir, program); err != nil {
		return testFailure(err, compiler.CodeTest, *structured)
	}
	if *structured {
		if err := testEvent("suite_start", map[string]any{"total": len(tests)}); err != nil {
			return err
		}
	}
	failures := 0
	for _, test := range tests {
		if *structured {
			if err := testEvent("test_start", map[string]any{"name": test.Name, "location": testLocation(test)}); err != nil {
				return err
			}
		} else {
			fmt.Println("RUN", test.Name)
		}
		start := time.Now()
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
		if *structured {
			cmd.Stdout = os.Stderr
		}
		err := cmd.Run()
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		cancel()
		status, reason := "pass", ""
		if err != nil || timedOut {
			failures++
			status = "fail"
			if err != nil {
				reason = err.Error()
			}
		}
		if timedOut {
			status, reason = "timeout", fmt.Sprintf("timeout after %s", *timeout)
		}
		if *structured {
			if err := testEvent("test_end", map[string]any{"name": test.Name, "location": testLocation(test), "durationMillis": time.Since(start).Milliseconds(), "status": status, "reason": reason}); err != nil {
				return err
			}
		} else if status == "pass" {
			fmt.Println("PASS", test.Name)
		} else {
			fmt.Printf("FAIL %s (%s:%d): %s\n", test.Name, test.Filename, test.Line, reason)
		}
	}
	if *structured {
		if err := testSummary(len(tests), len(tests)-failures, failures, failures == 0); err != nil {
			return err
		}
		if failures > 0 {
			return reportedError{fmt.Errorf("test suite failed")}
		}
		return nil
	}
	fmt.Printf("Tests: %d passed, %d failed\n", len(tests)-failures, failures)
	if failures > 0 {
		return fmt.Errorf("test suite failed")
	}
	return nil
}
