#!/bin/sh
# Input must be the compiler produced by a SUCCESSFUL self-hosting proof.
set -eu
[ "$#" = 3 ] || { echo 'Usage: package.sh VERIFIED_COMPILER VERSION OUTPUT_DIRECTORY' >&2; exit 1; }
compiler=$1
version=$2
output=$3
case "$version" in ''|*[!a-zA-Z0-9.+-]*) echo 'Invalid version' >&2; exit 1 ;; esac
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
[ -f "$compiler" ] || { echo 'Compiler missing' >&2; exit 1; }
otp=$(erl +S 1 -noshell -eval 'io:put_chars(erlang:system_info(otp_release)),halt().')
[ "$otp" = 29 ] || { echo 'Portable releases currently require OTP 29' >&2; exit 1; }
mkdir -p "$output"
output=$(CDPATH= cd -- "$output" && pwd)
name=linglang-$version-otp29
[ ! -e "$output/$name.tar.gz" ] || { echo 'Refusing to overwrite an archive' >&2; exit 1; }
work=$(mktemp -d "$output/.package.XXXXXX")
trap 'rm -rf "$work"' EXIT
trap 'exit 130' INT
trap 'exit 143' HUP TERM
mkdir -p "$work/$name/bin"
cp "$compiler" "$work/$name/bin/linglang"
chmod 755 "$work/$name/bin/linglang"
cp "$root/tools/release/install.sh" "$work/$name/install.sh"
chmod 755 "$work/$name/install.sh"
cp -R "$root/stdlib" "$root/book" "$root/README.md" "$work/$name/"
printf '%s\n' "$version" > "$work/$name/VERSION"
{
  printf 'version=%s\n' "$version"
  printf 'commit=%s\n' "$(git -C "$root" rev-parse HEAD)"
  if [ -n "$(git -C "$root" status --porcelain --untracked-files=normal)" ]; then
    printf 'source_dirty=true\n'
  else
    printf 'source_dirty=false\n'
  fi
  printf 'otp=%s\n' "$otp"
  printf 'otp_full=%s\n' "$(erl +S 1 -noshell -eval 'io:put_chars(erlang:system_info(system_version)),halt().')"
  printf 'format=self-hosted-escript\n'
} > "$work/$name/BUILD.txt"
# Suppress macOS resource forks in packages produced locally.
COPYFILE_DISABLE=1 tar -czf "$work/$name.tar.gz" -C "$work" "$name"
mv "$work/$name.tar.gz" "$output/$name.tar.gz"
(cd "$output" && if command -v sha256sum >/dev/null 2>&1; then sha256sum "$name.tar.gz"; else shasum -a 256 "$name.tar.gz"; fi) > "$output/$name.tar.gz.sha256"
echo "$output/$name.tar.gz"
