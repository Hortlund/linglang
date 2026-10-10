package compiler

import (
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"go/types"
)

// Codes identify broad error categories, not message wording. Keep their meaning
// stable so tools never need to parse English diagnostics.
const (
	CodeInput      = "LL1000"
	CodeSyntax     = "LL1001"
	CodeModule     = "LL1002"
	CodeType       = "LL1003"
	CodeLanguage   = "LL1004"
	CodeInternal   = "LL1099"
	CodeTest       = "LL1005"
	CodeDependency = "LL1006"
)

// SourceLocation always names physical source, ignoring //line directives.
// Offset is zero-based bytes; Line and Column are one-based (Column is bytes).
type SourceLocation struct {
	File   string `json:"file"`
	Offset int    `json:"offset"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type RelatedLocation struct {
	Message  string         `json:"message"`
	Location SourceLocation `json:"location"`
}

// Issue is independent of CLI rendering and editor coordinate encodings.
// A nil Location means no trustworthy source position is available.
type Issue struct {
	Code     string            `json:"code"`
	Severity string            `json:"severity"`
	Message  string            `json:"message"`
	Location *SourceLocation   `json:"location"`
	Related  []RelatedLocation `json:"related"`
}

type diagnosticError struct {
	issue Issue
	cause error
}

func (e *diagnosticError) Error() string {
	if p := e.issue.Location; p != nil {
		return fmt.Sprintf("%s:%d:%d: %s", p.File, p.Line, p.Column, e.issue.Message)
	}
	return e.issue.Message
}

func (e *diagnosticError) Unwrap() error { return e.cause }

func location(p token.Position) *SourceLocation {
	if !p.IsValid() {
		return nil
	}
	return &SourceLocation{p.Filename, p.Offset, p.Line, p.Column}
}

func errorAt(code string, p token.Position, message string) error {
	return &diagnosticError{issue: Issue{code, "error", message, location(p), []RelatedLocation{}}}
}

// IssueForError preserves typed diagnostics through wrapping. Unpositioned I/O
// or invocation failures retain their message without guessing a source span.
func IssueForError(err error, fallbackCode string) Issue {
	var diagnostic *diagnosticError
	if errors.As(err, &diagnostic) {
		issue := diagnostic.issue
		issue.Related = append([]RelatedLocation{}, issue.Related...)
		if issue.Location != nil {
			copy := *issue.Location
			issue.Location = &copy
		}
		return issue
	}
	return Issue{fallbackCode, "error", err.Error(), nil, []RelatedLocation{}}
}

func typeError(err error) error {
	var issue types.Error
	if errors.As(err, &issue) {
		return errorAt(CodeType, issue.Fset.PositionFor(issue.Pos, false), issue.Msg)
	}
	return errorAt(CodeInternal, token.Position{}, err.Error())
}

func syntaxError(fset *token.FileSet, filename string, err error) error {
	var issues scanner.ErrorList
	if errors.As(err, &issues) && len(issues) > 0 {
		first := issues[0]
		var position token.Position
		// Parser positions may have a redirected filename/line. Locate the input
		// by physical filename and reconstruct the position using its byte offset.
		fset.Iterate(func(file *token.File) bool {
			if file.Name() == filename && first.Pos.Offset <= file.Size() {
				position = file.PositionFor(file.Pos(first.Pos.Offset), false)
			}
			return true
		})
		return errorAt(CodeSyntax, position, first.Msg)
	}
	return errorAt(CodeInput, token.Position{}, err.Error())
}

func importError(at token.Position, err error) error {
	issue := IssueForError(err, CodeModule)
	if issue.Location == nil {
		issue.Location = location(at)
	} else if related := location(at); related != nil {
		issue.Related = append(issue.Related, RelatedLocation{"imported here", *related})
	}
	return &diagnosticError{issue: issue, cause: err}
}
