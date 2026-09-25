#!/bin/sh
# Builds libkrun as third_party/libkrun.lock pins it, for the host, into the directory given as
# the first argument: libkrun.1.dylib on macOS, libkrun.so.1 on Linux, named as cove-vmm links it.
#
# Nothing of libkrun is kept in the repository. The source is fetched by its commit, from each URL
# of the lock in turn, and refused unless its git archive hashes to the sha256 of the lock. The
# crates are fetched by cargo, which checks each one against the checksum of Cargo.lock, and the
# lock of the source must be the one committed beside this script. The build itself runs offline.
# What is fetched is kept under $COVE_BUILD_CACHE, keyed by the commit, so a second build fetches
# nothing.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
out=${1:?usage: build.sh OUTPUT_DIR}
# shellcheck source=../libkrun.lock
. "$here/../libkrun.lock"

cache=${COVE_BUILD_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/cove-build}/libkrun/$LIBKRUN_COMMIT
src=$cache/src

sha256() { shasum -a 256 | cut -d ' ' -f 1; }

fetch() {
	rm -rf "$cache/git" "$src"
	mkdir -p "$cache/git" "$src"
	git -C "$cache/git" init -q
	for url in $LIBKRUN_URLS; do
		if git -C "$cache/git" fetch -q --depth 1 "$url" "$LIBKRUN_COMMIT"; then
			break
		fi
		echo "libkrun: $url did not serve $LIBKRUN_COMMIT" >&2
	done
	# The archive is what is built, so it is what the sha256 names; the commit names the history.
	got=$(git -C "$cache/git" archive --format=tar "$LIBKRUN_COMMIT" | sha256)
	if [ "$got" != "$LIBKRUN_ARCHIVE_SHA256" ]; then
		echo "libkrun: the archive of $LIBKRUN_COMMIT hashes to $got, the lock says $LIBKRUN_ARCHIVE_SHA256" >&2
		exit 1
	fi
	git -C "$cache/git" archive --format=tar "$LIBKRUN_COMMIT" | tar -x -C "$src"
	rm -rf "$cache/git"
}

[ -f "$src/Cargo.lock" ] || fetch

if ! cmp -s "$src/Cargo.lock" "$here/Cargo.lock"; then
	echo "libkrun: the Cargo.lock of $LIBKRUN_COMMIT is not third_party/libkrun/Cargo.lock" >&2
	exit 1
fi
case $(rustc --version) in
"rustc $RUST_VERSION "*) ;;
*)
	echo "libkrun: the lock builds with rustc $RUST_VERSION, not $(rustc --version)" >&2
	exit 1
	;;
esac

export CARGO_HOME="$cache/cargo"
export CARGO_TARGET_DIR="$cache/target"
# The paths of the build machine would otherwise be written into the library, and two machines
# would give two libraries.
export RUSTFLAGS="--remap-path-prefix=$src=/libkrun --remap-path-prefix=$CARGO_HOME=/cargo"
host=$(rustc -vV | sed -n 's/^host: //p')
(cd "$src" && cargo fetch --locked --target "$host" -q)
(cd "$src" && cargo build --release --locked --offline -q \
	-p libkrun --no-default-features --features "$LIBKRUN_FEATURES")

mkdir -p "$out"
case $(uname -s) in
Darwin)
	lib=libkrun.1.dylib
	cp "$CARGO_TARGET_DIR/release/libkrun.dylib" "$out/$lib"
	# cove-vmm finds the library beside it, whatever directory it is installed in; the new name
	# voids the signature the linker gave, which arm64 requires, so it is signed again.
	install_name_tool -id "@rpath/$lib" "$out/$lib"
	codesign -s - -f "$out/$lib" 2>/dev/null
	;;
Linux)
	lib=libkrun.so.1
	cp "$CARGO_TARGET_DIR/release/libkrun.so" "$out/$lib"
	;;
esac
echo "$(sha256 <"$out/$lib")  $lib"
