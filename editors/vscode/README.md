# Linglang for VS Code

The bean has colours now. Errors too. Those were already there.

Open `.lang` files for syntax highlighting, comments, bracket pairing, and snippets.
The bundled language server adds syntax/name/type diagnostics, formatting, and an
outline of functions, structs, fields, and constants. Unsaved buffers in the same
directory are checked together, including tests. Put a `.linglang-standalone`
file in a directory of independent programs to check each open file separately.
The marker applies only to that directory, so nested packages still work. Its
contents are ignored; create/delete it to switch modes. The repository marks
`examples/` and `benchmarks/` this way. Erlang is needed to run programs;
editor analysis does not launch it.

Install the VSIX through **Extensions: Install from VSIX…**, or:

```sh
code --install-extension dist/linglang-vscode-darwin-arm64.vsix
```

Each VSIX bundles the Linglang executable for its target OS and architecture.
Set `linglang.serverPath` to use a different compiler build; reload the window
after changing it. An unpackaged development extension uses `linglang` on PATH
when this setting is empty. Only trusted workspaces start the server.

From the repository root, build a package with Node.js 22+ and Go:

```sh
cd editors/vscode
npm ci
npm test
npm run package
```

The default target is the build machine. An explicit target works too:

```sh
npm run package -- linux-x64
```

Supported targets are `darwin-arm64`, `darwin-x64`, `linux-arm64`, `linux-x64`,
`win32-arm64`, and `win32-x64`. Output is in the repository's `dist/` directory.
Packaging is local; it does not publish to the Marketplace.

With VS Code installed, test the packaged extension in an isolated editor:

```sh
npm run package
npm run test:editor -- "/Applications/Visual Studio Code.app/Contents/MacOS/Code"
```

Pass the native VS Code executable on other systems, or set `VSCODE_EXECUTABLE`.
The test uses a temporary profile and checks real activation, Unicode diagnostics,
outline, formatting, and unsaved updates without changing your editor setup.
Build the host target before running it. CI checks the grammar and Linux packaging;
the editor session requires a graphical desktop.

The first server uses the Go seed frontend and the CLI's formatter. It tolerates
unfinished syntax for outlines; type analysis resumes once syntax is repaired.
Diagnostics use LSP UTF-16 positions and physical source locations, including
Unicode and CRLF. Advanced message/BEAM lowering restrictions remain build-time
checks. Completion, hover, references, rename, and debugging are later milestones.
