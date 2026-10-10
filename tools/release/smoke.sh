#!/bin/sh
# Exercise the exact downloaded archive, with Go and erlc absent from PATH.
set -eu
[ "$#" = 1 ] || { echo 'Usage: smoke.sh ARTIFACT_DIRECTORY' >&2; exit 1; }
artifacts=$(CDPATH= cd -- "$1" && pwd)
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
(cd "$artifacts" && if command -v sha256sum >/dev/null 2>&1; then sha256sum -c ./*.sha256; else shasum -a 256 -c ./*.sha256; fi)
set -- "$artifacts"/*.tar.gz
[ "$#" = 1 ] || { echo 'Expected one compiler archive' >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
trap 'exit 130' INT
trap 'exit 143' HUP TERM
mkdir "$work/extracted" "$work/path"
tar -xzf "$1" -C "$work/extracted"
set -- "$work/extracted"/*
[ "$#" = 1 ] && [ -d "$1" ]
bundle=$1
for tool in erl escript dirname basename cat cp chmod mkdir mktemp mv ln rm awk; do
  target=$(command -v "$tool")
  # OTP launchers must see their original location, not a relocated symlink.
  quoted=$(printf '%s' "$target" | sed "s/'/'\\\\''/g")
  printf '#!/bin/sh\nexec '\''%s'\'' "$@"\n' "$quoted" > "$work/path/$tool"
  chmod +x "$work/path/$tool"
done
export PATH="$work/path"
unset ERL_LIBS ERL_FLAGS ERL_AFLAGS ERL_ZFLAGS ERL_ROOTDIR ROOTDIR BINDIR EMU PROGNAME ESCRIPT_EMULATOR
for tool in go erlc; do
  if command -v "$tool" >/dev/null 2>&1; then
    echo "Unexpected $tool on isolated PATH" >&2
    exit 1
  fi
done
prefix="$work/install with spaces"
"$bundle/install.sh" --prefix "$prefix"
cli=$prefix/bin/linglang
export PATH="$prefix/bin:$PATH"
"$cli" describe --json
# Execute the actual generated instructions, not a second hand-maintained list.
# Each guide starts in a fresh project so README formatting/locks cannot mask
# missing setup in AGENTS.md (or vice versa). The installed name is on PATH;
# neither a Go seed nor a linglang-bootstrap alias is available.
if command -v linglang-bootstrap >/dev/null 2>&1; then
  echo 'Unexpected linglang-bootstrap alias on isolated PATH' >&2
  exit 1
fi
for guide in README.md AGENTS.md; do
  project="$work/$guide project"
  "$cli" init "$project"
  instructions="$work/$guide.sh"
  awk '
    /^```sh$/ { shell = 1; blocks++; next }
    /^```$/ { shell = 0; next }
    shell { print; if ($0 !~ /^[[:space:]]*$/) lines++ }
    END { if (blocks == 0 || lines == 0 || shell) exit 1 }
  ' "$project/$guide" > "$instructions"
  (cd "$project" && /bin/sh -eu "$instructions")
  if [ "$guide" = README.md ]; then
    [ "$("$project/counter.escript")" = 'Counter: 5' ]
  fi
done
"$cli" test --gc-stress "$root/tests/language"
"$cli" test --no-opt --gc-stress "$root/tests/language"
# A failed reinstall leaves the working compiler in place.
if "$bundle/install.sh" --prefix "$prefix"; then echo 'Reinstall should refuse existing version' >&2; exit 1; fi
"$cli" check "$project"
echo 'Toolchain installation and execution passed without Go or erlc.'
