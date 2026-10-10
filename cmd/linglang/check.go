package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"linglang/internal/compiler"
)

// reportedError keeps run's error contract while telling main that a structured
// diagnostic has already been written. Do not mix English errors into JSON.
type reportedError struct{ error }

func (e reportedError) Unwrap() error { return e.error }

type checkReport struct {
	SchemaVersion int              `json:"schemaVersion"`
	Command       string           `json:"command"`
	Compiler      string           `json:"compiler"`
	Mode          string           `json:"mode"`
	OK            bool             `json:"ok"`
	SourceFiles   int              `json:"sourceFiles"`
	Diagnostics   []compiler.Issue `json:"diagnostics"`
}

func checkCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "write a versioned JSON report")
	tests := flags.Bool("tests", false, "check test files and signatures without executing tests")
	noOpt := flags.Bool("no-opt", false, "use the original cell-based lowering for comparisons")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("check expects one source file or directory")
	}
	report := checkReport{SchemaVersion: 1, Command: "check", Compiler: "go-seed", Mode: "program", Diagnostics: []compiler.Issue{}}
	if *tests {
		report.Mode = "tests"
	}
	sources, err := readPackageSources(flags.Arg(0), *tests)
	report.SourceFiles = len(sources)
	code := compiler.CodeInput
	if err == nil {
		code = compiler.CodeInternal
		options := compiler.Options{DisableOptimizations: *noOpt}
		if *tests {
			_, _, err = compiler.CompileTestFilesWithOptions(sources, options)
		} else {
			_, err = compiler.CompileFilesWithOptions(sources, options)
		}
	}
	report.OK = err == nil
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, compiler.IssueForError(err, code))
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if writeErr := encoder.Encode(report); writeErr != nil {
			return writeErr
		}
		if err != nil {
			return reportedError{err}
		}
		return nil
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Checked %s (%d source files)\n", flags.Arg(0), len(sources))
	return err
}
