package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"linglang/internal/compiler"
)

type commandDescription struct {
	Name    string `json:"name"`
	Usage   string `json:"usage"`
	Summary string `json:"summary"`
}

// Human help and capability discovery share a command catalogue.
var commandDescriptions = []commandDescription{
	{"init", "init [directory]", "Create a project in an empty directory."},
	{"describe", "describe [--json]", "Describe this driver's capabilities and native API without running programs."},
	{"deps", "deps [--check] [--json] [file.lang|directory]", "Record or verify local dependency hashes; does not fetch packages."},
	{"run", "run [--no-opt] [--gc-stats] [--gc-stress] <file.lang|directory> [args...]", "Compile and execute a program; requires erl and erlc."},
	{"check", "check [--json] [--tests] [--no-opt] <file.lang|directory>", "Validate through lowering without running a program or requiring OTP."},
	{"build", "build [--no-opt] [-o directory] <file.lang|directory>", "Write BEAM modules; requires erlc."},
	{"pack", "pack [--no-opt] [--gc-stats] [--gc-stress] [-o executable] <file.lang|directory>", "Create an executable requiring OTP; requires erl and erlc to package."},
	{"release", "release [--no-opt] [--gc-stats] [--gc-stress] [-o archive.tar.gz] <file.lang|directory>", "Bundle the current platform's OTP runtime with a program."},
	{"emit", "emit [--no-opt] <file.lang|directory>", "Print Erlang source without requiring OTP."},
	{"test", "test [--json] [--no-opt] [--gc-stats] [--gc-stress] [--timeout 30s] [file_test.lang|directory]", "Run root Test functions in separate VMs with per-test deadlines; requires OTP."},
	{"fmt", "fmt [--check] [file.lang|directory ...]", "Format source or check formatting without writing."},
	{"lsp", "lsp", "Serve the editor protocol over standard input/output."},
}

type capability struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Notes  string `json:"notes"`
}

type toolDescription struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Compiler      string                       `json:"compiler"`
	Stability     string                       `json:"stability"`
	Commands      []commandDescription         `json:"commands"`
	Capabilities  []capability                 `json:"capabilities"`
	NativeAPI     []compiler.NativeDeclaration `json:"nativeAPI"`
	Notes         []string                     `json:"notes"`
}

func commandUsage() string {
	var text strings.Builder
	text.WriteString("Usage:\n")
	for _, command := range commandDescriptions {
		fmt.Fprintln(&text, "  linglang", command.Usage)
	}
	return text.String()
}

func describeCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("describe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "write a versioned JSON capability catalogue")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("describe accepts no source arguments")
	}
	api, err := compiler.NativeAPI()
	if err != nil {
		return err
	}
	description := toolDescription{
		SchemaVersion: 1, Compiler: "go-seed", Stability: "experimental",
		Commands: commandDescriptions, NativeAPI: api,
		Capabilities: []capability{
			{"structured-check", "supported", "check --json, schema version 1; first error, full lowering; --tests checks library/test roots without execution."},
			{"structured-tests", "supported", "test --json emits version-1 JSON Lines progress/results; child stdout and stderr go to stderr."},
			{"local-imports", "supported", "Relative directory imports; ASCII uppercase exports; file-local aliases; one linked BEAM module."},
			{"dependency-lock", "supported", "Explicit deps --check verifies local source hashes; builds do not enforce the lock automatically."},
			{"otp-processes", "supported", "Typed messages, monitors, timers, supervisors, synchronous servers; process-local pointers cannot cross process boundaries."},
			{"tcp", "supported", "Passive, owner-local sockets with typed I/O results."},
			{"sqlite", "supported", "Requires linglang-sqlite on PATH; one connection and statement per call, prepared parameters, no multi-call transactions."},
			{"http", "limited", "Source library stdlib/http: bounded single-request HTTP/1.1 subset, no request bodies or TLS."},
			{"remote-packages", "unavailable", "No fetching, registry, version solver, or package install hooks."},
			{"user-generics", "unavailable", "Generic native operations exist; user-defined generic declarations are unsupported."},
			{"self-hosted-tooling", "limited", "The separate bootstrap driver supports check/emit/build/run/test/fmt/init/describe/deps; lsp and release remain Go CLI features; both drivers support JSON checks/tests and check --tests."},
		},
		Notes: []string{
			"This describes compiled capabilities, not installed runtime dependencies or a compatibility guarantee.",
			"nativeAPI is derived from the checking prelude. Generic declarations describe native signatures, not syntax for user-defined generics. Opaque type representations are hidden.",
			"Operators and language builtins such as len, append, print, println, new, make, and panic are documented in book/src/builtins.md, not nativeAPI.",
			"See book/src/agent-workflows.md for automation, book/src/specification.md for semantics, and book/src/architecture.md for compiler boundaries.",
		},
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(description)
	}
	var text strings.Builder
	text.WriteString("Linglang — Go seed driver (experimental)\n\n")
	text.WriteString(commandUsage())
	text.WriteString("\nCapabilities:\n")
	for _, feature := range description.Capabilities {
		fmt.Fprintf(&text, "  %s: %s — %s\n", feature.Name, feature.Status, feature.Notes)
	}
	text.WriteString("\nNative API:\n")
	for _, declaration := range api {
		fmt.Fprintln(&text, declaration.Declaration)
	}
	for _, note := range description.Notes {
		fmt.Fprintln(&text, "\n"+note)
	}
	_, err = io.WriteString(stdout, text.String())
	return err
}
