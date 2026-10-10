package main

import (
	"encoding/json"
	"os"

	"linglang/internal/compiler"
)

// JSON Lines are emitted synchronously around each test. Child output goes to
// stderr at the descriptor boundary, so arbitrary program bytes cannot corrupt
// the protocol. No unbounded output buffers are retained by the runner.
func testEvent(event string, fields map[string]any) error {
	fields["schemaVersion"] = 1
	fields["command"] = "test"
	fields["compiler"] = "go-seed"
	fields["event"] = event
	return json.NewEncoder(os.Stdout).Encode(fields)
}

func testLocation(test compiler.TestCase) compiler.SourceLocation {
	return compiler.SourceLocation{File: test.Filename, Offset: test.Offset, Line: test.Line, Column: test.Column}
}

func testSummary(total, passed, failed int, ok bool) error {
	return testEvent("suite_end", map[string]any{"total": total, "passed": passed, "failed": failed, "ok": ok})
}

func testFailure(err error, code string, structured bool) error {
	if !structured {
		return err
	}
	if writeErr := testEvent("diagnostic", map[string]any{"diagnostic": compiler.IssueForError(err, code)}); writeErr != nil {
		return writeErr
	}
	if writeErr := testSummary(0, 0, 0, false); writeErr != nil {
		return writeErr
	}
	return reportedError{err}
}
