# Install and release the toolchain

## Install a compiler download

The release workflow builds a **self-hosted compiler**, named `linglang`, as a
portable escript archive. It needs Erlang/OTP **29**, including the `compiler`
and `crypto` applications, with `erl` and `escript` on PATH. It does not require
Go or the external `erlc` executable. The release installation checks run on
Linux x86-64 and macOS ARM64. Other platforms are not release-tested yet.

Install OTP from your platform's packages or your usual OTP version manager.
Check the major version with:

```sh
erl +S 1 -noshell -eval 'io:format("~s~n", [erlang:system_info(otp_release)]), halt().'
```

Open [GitHub Releases](https://github.com/Hortlund/linglang/releases) and choose
an explicitly published version. Until a maintainer publishes the first release,
use the [source build](getting-started.md#build-the-development-compiler).
Download both `linglang-VERSION-otp29.tar.gz` and its `.sha256` file into an empty
directory. Replace `VERSION` below with that exact version, including its `v`
prefix. Verify the archive **before** extracting or running it:

```sh
# Linux:
sha256sum -c linglang-VERSION-otp29.tar.gz.sha256
# macOS alternative:
shasum -a 256 -c linglang-VERSION-otp29.tar.gz.sha256

tar -xzf linglang-VERSION-otp29.tar.gz
cd linglang-VERSION-otp29
./install.sh
export PATH="$HOME/.local/bin:$PATH"
linglang describe
linglang init hello
cd hello
linglang test --json .
linglang run .
linglang build -o hello.escript .
./hello.escript
```

The checksums detect damaged or substituted files when compared with the trusted
release's checksum file; they are not separate publisher signatures.

The installer defaults to `~/.local` and needs no `sudo`. Use
`./install.sh --prefix /absolute/path` to choose another location. Add its `bin`
directory to your shell's PATH permanently if desired. The executable link is
replaced only after the new compiler passes a discovery check. Each version's
compiler, source libraries, book, version, and build metadata remain in
`PREFIX/lib/linglang/VERSION`. Installing a version that already exists is refused.

You can also run `bin/linglang` directly from the extracted archive. Relative
imports remain relative to your source files; there is no implicit global
standard-library import path. Copy needed packages from the bundled `stdlib`
into your project's vendor directory, then use relative imports and `deps`.

The archive does not include the optional SQLite helper, the Go seed's `lsp` or
`release` commands, or OTP itself. See [SQLite setup](io.md) and
[application shipping](tooling.md). The downloaded compiler's `build` creates an
OTP-dependent executable; its format differs from the Go seed's `build`
directory output. VS Code's existing extension still uses its Go-based server.

To roll back, point `PREFIX/bin/linglang` at a retained version's
`bin/linglang`. To uninstall, remove that link and the version directories you
installed. Removing a compiler does not remove your projects.

## Maintainer release procedure

`.github/workflows/release.yml` runs manually or when a `v*` tag is pushed.
Manual branch runs produce `dev-COMMIT` downloadable workflow artifacts without
creating a GitHub release. Tag names must have the form `vMAJOR.MINOR.PATCH` or
`vMAJOR.MINOR.PATCH-suffix`.

The workflow:

1. Checks Go formatting, vets, and runs the repository test suite on Linux/OTP 29.
2. Builds compiler A from source, uses A to build B, then B to build C, and
   requires byte-identical B/C Erlang emission. It executes the actual C through
   the full self-hosted driver acceptance tests and both lowering modes.
3. Runs the Linglang frontend, concurrency, and language suites with C and forced
   collection, including the multiple-result regressions.
4. Packages C, source libraries, the book sources, `VERSION`, and `BUILD.txt`,
   emits SHA-256 checksums, and saves the self-hosting proof log.
5. Downloads the same archive on Linux and macOS, verifies its checksum, and
   tests installation without Go or `erlc` on PATH. It runs the shell command
   blocks from each generated starter README and AGENTS guide in separate fresh
   projects, using the installed `linglang` command, then checks the built app.
6. For a tag, creates a **draft prerelease** only after both installation jobs
   pass. Inspect the notes, assets, proof log, and compatibility evidence before
   publishing it in GitHub. A branch run never publishes anything.

For an intended version (example only):

```sh
git tag -a v0.1.0 -m "Linglang v0.1.0"
git push origin v0.1.0
```

Tags should identify the reviewed commit. Do not move a published tag or replace
published artifacts; make a new version for fixes. The workflow refuses to
recreate an existing release instead of overwriting it. If a draft was left by
an interrupted upload, inspect and delete that draft before rerunning.

The release gate currently targets OTP 29. Source compatibility testing on
OTP 27–29 is a separate CI matrix; compiling BEAM files on OTP 29 does not imply
those files run on earlier OTP versions. Bundled artifacts include no native
code, but portability beyond the tested platforms is not claimed. Releases
remain experimental until language and tool contracts stabilize.

For local packaging after a successful proof:

```sh
LINGLANG_SELFHOST=1 LINGLANG_SELFHOST_OUT="$PWD/bin/linglang-next" \
  go test ./cmd/linglang -run '^TestBootstrapSelfHostProof$' -count=1 -v -timeout=45m
# Only continue if the complete proof succeeds:
tools/release/package.sh bin/linglang-next dev-local dist/local-toolchain
tools/release/smoke.sh dist/local-toolchain
```

`package.sh` packages the supplied compiler; it does not replace or repeat the
proof. Use a fresh output directory and version. Installation tests use temporary
prefixes and leave your installed compiler alone.
