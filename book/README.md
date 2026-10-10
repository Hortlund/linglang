# Building the book

This is the maintained user documentation. `docs/` contains ignored local
handoffs; it is not the source of the language book.

Use [mdBook](https://rust-lang.github.io/mdBook/guide/installation.html)
(tested with 0.5.2):

```sh
mdbook build book
mdbook serve book --hostname 127.0.0.1 --port 3000
```

Run from the repository root. HTML goes to `_build/book/`. The site includes
chapter navigation, search, and light/dark themes. No publishing is needed to
read it locally. `mdbook test` is for Rust examples; use our example checks:

```sh
go test ./cmd/linglang -run 'TestBookExamples|TestInitProject' -count=1
```

The chapters include actual `.lang` source files rather than copies. Keep the
capability matrix and draft specification aligned with compiler tests. Planned
features must be labelled as planned. Add chapters to `src/SUMMARY.md`.
