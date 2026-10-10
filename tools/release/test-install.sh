#!/bin/sh
# Fast filesystem regressions; smoke.sh separately tests a real verified compiler.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
trap 'exit 130' INT
trap 'exit 143' HUP TERM
mkdir -p "$work/otp" "$work/package/bin" "$work/package/book" "$work/package/stdlib"
printf '#!/bin/sh\nexit 0\n' > "$work/otp/erl"
printf '#!/bin/sh\nexit 0\n' > "$work/otp/escript"
chmod +x "$work/otp/erl" "$work/otp/escript"
export PATH="$work/otp:$PATH"
cp "$root/install.sh" "$work/package/install.sh"
printf '#!/bin/sh\nexit 0\n' > "$work/package/bin/linglang"
printf 'v0.0.1-test\n' > "$work/package/VERSION"
touch "$work/package/README.md" "$work/package/BUILD.txt"
prefix="$work/installation's 雪 space"
"$work/package/install.sh" --prefix "$prefix"
[ -L "$prefix/bin/linglang" ]
[ -f "$prefix/lib/linglang/v0.0.1-test/BUILD.txt" ]
original=$(readlink "$prefix/bin/linglang")
if "$work/package/install.sh" --prefix "$prefix"; then exit 1; fi
[ "$(readlink "$prefix/bin/linglang")" = "$original" ]
# Reject an unusable compiler without changing the previous installation.
printf 'v0.0.2-test\n' > "$work/package/VERSION"
printf '#!/bin/sh\nexit 42\n' > "$work/package/bin/linglang"
if "$work/package/install.sh" --prefix "$prefix"; then exit 1; fi
[ "$(readlink "$prefix/bin/linglang")" = "$original" ]
[ ! -e "$prefix/lib/linglang/v0.0.2-test" ]
# Successful upgrade retains the previous version for rollback.
printf '#!/bin/sh\nexit 0\n' > "$work/package/bin/linglang"
"$work/package/install.sh" --prefix "$prefix"
[ -f "$original" ]
[ "$(readlink "$prefix/bin/linglang")" != "$original" ]
# Missing/incompatible OTP must be detected before a destination is created.
printf '#!/bin/sh\nexit 1\n' > "$work/otp/erl"
if "$work/package/install.sh" --prefix "$work/bad-otp"; then exit 1; fi
[ ! -e "$work/bad-otp" ]
if "$work/package/install.sh" --prefix relative; then exit 1; fi
# Preserve a directory at the executable path.
mkdir -p "$work/directory/bin/linglang"
printf '#!/bin/sh\nexit 0\n' > "$work/otp/erl"
if "$work/package/install.sh" --prefix "$work/directory"; then exit 1; fi
[ -d "$work/directory/bin/linglang" ]
echo 'Installer regression tests passed.'
