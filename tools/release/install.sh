#!/bin/sh
# Install an extracted, checksum-verified toolchain. No network or sudo required.
set -eu
fail() { echo "install: $*" >&2; exit 1; }
prefix=${HOME:?}/.local
case $# in
  0) ;;
  1) [ "$1" = --help ] || fail 'usage: ./install.sh [--prefix /absolute/path]'
     echo 'Usage: ./install.sh [--prefix /absolute/path] (default: ~/.local)'; exit 0 ;;
  2) [ "$1" = --prefix ] || fail 'expected --prefix'; prefix=$2 ;;
  *) fail 'usage: ./install.sh [--prefix /absolute/path]' ;;
esac
case "$prefix" in /*) ;; *) fail 'prefix must be an absolute path' ;; esac
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
version=$(cat "$root/VERSION")
case "$version" in ''|*[!a-zA-Z0-9.+-]*) fail 'invalid VERSION' ;; esac
command -v escript >/dev/null 2>&1 || fail 'install Erlang/OTP 29 first (escript missing)'
command -v erl >/dev/null 2>&1 || fail 'install Erlang/OTP 29 first (erl missing)'
erl +S 1 -noshell -eval 'case {erlang:system_info(otp_release),code:which(compile),code:which(crypto)} of {"29",C,H} when C =/= non_existing, H =/= non_existing -> halt(0); _ -> halt(1) end.' || fail 'this package requires full Erlang/OTP 29, including compiler and crypto'
[ -f "$root/bin/linglang" ] || fail 'compiler missing from package'
mkdir -p "$prefix/bin" "$prefix/lib/linglang"
destination=$prefix/lib/linglang/$version
[ ! -e "$destination" ] && [ ! -L "$destination" ] || fail "version already installed: $destination"
[ ! -d "$prefix/bin/linglang" ] || fail 'bin/linglang is a directory'
staging=$(mktemp -d "$prefix/lib/linglang/.install.XXXXXX")
linkdir=$(mktemp -d "$prefix/bin/.linglang-link.XXXXXX")
trap 'rm -rf "$staging" "$linkdir"' EXIT
trap 'exit 130' INT
trap 'exit 143' HUP TERM
cp -R "$root/bin" "$root/stdlib" "$root/book" "$root/README.md" "$root/VERSION" "$root/BUILD.txt" "$staging/"
chmod 755 "$staging/bin/linglang"
"$staging/bin/linglang" describe --json >/dev/null
mv "$staging" "$destination"
ln -s "$destination/bin/linglang" "$linkdir/linglang"
mv -f "$linkdir/linglang" "$prefix/bin/linglang"
echo "Installed Linglang $version in $destination"
echo "Add $prefix/bin to PATH, then run: linglang init hello"
