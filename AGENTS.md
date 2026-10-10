# Working on Linglang

Linglang is an experimental imperative language on BEAM, with a Go seed compiler
and a compiler written in Linglang. Keep both implementations and the book in
agreement. Be direct and practical; explain behavior and evidence.

## Start here

- `book/src/specification.md`: implemented language contract, including limitations.
- `book/src/architecture.md`: compiler stages, linking, runtime boundaries.
- `book/src/agent-workflows.md`: tool protocol and a change-location map.
- `book/src/status.md`: seed/bootstrap differences and acceptance criteria.
- `book/src/self-hosting.md`: rebuild an existing compiler using OTP without Go.
- `book/src/scaling.md`: bounded concurrency and failure/recovery boundaries.
- `bin/linglang-bootstrap describe --json`: installed self-hosted commands/API.
  `go run ./cmd/linglang describe --json` describes the current seed sources.

Prefer source and tests over ignored `bin/` or `_build/` artifacts, which may be
stale. Bootstrap stages share source through symlinks: edit the owning file and
preserve the links. Do not replace links with copies.

## Implement and verify

1. Specify the behavior: syntax, typing, evaluation order, failure, ownership.
2. Find both implementations using the change map. Language changes normally
   need the seed and bootstrap; CLI-only additions should say which driver owns them.
3. Add focused positive and negative regression cases. Use both lowering modes
   for semantic changes and forced GC for pointer/process/lifetime changes.
4. Update the specification and relevant book chapter with implemented behavior.
5. Run focused tests, then the relevant integration checks in
   `book/src/contributing.md`. Compiler/runtime/bootstrap changes require the
   fixed-point proof before claiming a newly built generation C is verified.

Useful fast checks:

```sh
go test ./internal/compiler -run '<focused test>' -count=1
go test ./cmd/linglang -run 'TestStructuredToolingCLI|TestCheckTestDiscovery' -count=1
go vet ./...
git diff --check
mdbook build book
```

`check --json` validates through lowering; the editor's `Analyze` is partial
analysis and cannot replace it. `check --tests` validates tests without running
them. Run the actual tests before claiming their assertions pass. The Go CLI checks
work without OTP; the self-hosted driver and program execution require OTP.
Both drivers support `test --json` event streams; keep stdout protocol-only and
preserve inherited stdin, stderr output, and child lifetime on cancellation.

Keep machine output versioned and deterministic. Preserve typed error locations
at stage boundaries; never recover locations or error categories by scraping
English messages. Do not hide unsupported language features behind the Go
parser's broader syntax or expose prelude implementation types as language types.

The self-hosted driver also owns `fmt`, `init`, `describe`, and `deps`. Its whitespace style differs
from the Go printer; choose a formatter for a source tree instead of alternating
check policies. Repository compiler sources currently use the Go printer style.
Keep `project_templates.lang` synchronized with `cmd/linglang/starter/`; the
OTP-only developer-tools tests enforce byte parity. New formatting changes must
preserve parsed syntax, comments, literals, idempotence, and atomic file writes.

Discovery metadata must describe the invoked driver. Keep its generic intrinsic
schemes/parameter labels aligned with native checking; differential catalogue
tests detect drift. Both drivers share lock version 1 and deps report version 1.
Lock verification must never mutate files, execute code, or fetch dependencies.
