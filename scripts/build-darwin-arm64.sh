#!/usr/bin/env bash
#
# Native macOS Apple Silicon (darwin/arm64) build for Elementum.
#
# Upstream only ships Docker cross-compiler images for darwin-x64; there is no
# darwin-arm64 image. This script reproduces the toolchain natively:
#   - Boost 1.72.0 built from source (libtorrent 1.1.x needs the deprecated
#     boost/asio/io_service.hpp removed in modern Boost).
#   - libtorrent-rasterbar 1.1.14 (pinned commit) built static from source.
#   - OpenSSL linked statically from Homebrew archives (self-contained binary).
#   - A local copy of ElementumOrg/libtorrent-go with its go.mod bumped to
#     go 1.17 (the SWIG-generated bindings use unsafe.Slice).
#
# Output: build/darwin_arm64/elementum (+ appended sha checksum line).
#
# Usage:
#   scripts/build-darwin-arm64.sh
#
# Tunables (env):
#   WORKDIR        scratch dir for sources/prefixes (default: .build-darwin-arm64)
#   OUTPUT_DIR     where to place the binary (default: build/darwin_arm64)
#   SKIP_BREW=1    skip `brew install` of build dependencies
#   JOBS           parallelism (default: number of CPUs)

set -euo pipefail

# --- Configuration ---------------------------------------------------------
BOOST_VERSION=1.72.0
LIBTORRENT_COMMIT=760f94862ef6b76a13bba0a68d55ca6507aef7c2  # RC_1_1 (1.1.14)
LIBTORRENT_GO_VERSION=v0.0.0-20230915150218-d8763f5e1783

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="${WORKDIR:-$REPO_ROOT/.build-darwin-arm64}"
OUTPUT_DIR="${OUTPUT_DIR:-$REPO_ROOT/build/darwin_arm64}"
JOBS="${JOBS:-$(sysctl -n hw.ncpu 2>/dev/null || echo 4)}"

BREW_PREFIX="$(brew --prefix)"
OPENSSL_DIR="$(brew --prefix openssl@3)"

BOOST_PREFIX="$WORKDIR/boost-prefix"
LT_PREFIX="$WORKDIR/lt-prefix"
LTGO_LOCAL="$WORKDIR/libtorrent-go-local"
PC_DIR="$WORKDIR/pkgconfig"

echo ">>> Elementum darwin/arm64 native build"
echo "    repo:      $REPO_ROOT"
echo "    workdir:   $WORKDIR"
echo "    output:    $OUTPUT_DIR"
echo "    jobs:      $JOBS"
echo "    openssl:   $OPENSSL_DIR"

if [ "$(uname -s)" != "Darwin" ] || [ "$(uname -m)" != "arm64" ]; then
  echo "!!! This script must run on macOS Apple Silicon (arm64)." >&2
  exit 1
fi

mkdir -p "$WORKDIR"

# --- 1. Build dependencies -------------------------------------------------
if [ "${SKIP_BREW:-}" != "1" ]; then
  echo ">>> Installing build dependencies via Homebrew"
  brew install swig pkg-config openssl@3 autoconf automake libtool wget
fi

export PATH="$BREW_PREFIX/bin:$PATH"

# --- 2. Boost 1.72 (static, arm64) -----------------------------------------
if [ ! -f "$BOOST_PREFIX/lib/libboost_system.a" ]; then
  echo ">>> Building Boost $BOOST_VERSION from source"
  BOOST_FILE="boost_${BOOST_VERSION//./_}"
  cd "$WORKDIR"
  if [ ! -d "$BOOST_FILE" ]; then
    curl -fsSL -o "$BOOST_FILE.tar.gz" \
      "https://archives.boost.io/release/${BOOST_VERSION}/source/${BOOST_FILE}.tar.gz"
    tar -xzf "$BOOST_FILE.tar.gz"
  fi
  cd "$BOOST_FILE"
  ./bootstrap.sh --with-toolset=clang \
    --with-libraries=system,chrono,random,date_time
  ./b2 -j"$JOBS" \
    --prefix="$BOOST_PREFIX" \
    --with-system --with-chrono --with-random --with-date_time \
    toolset=clang \
    cxxflags="-arch arm64 -std=c++14" \
    cflags="-arch arm64" \
    linkflags="-arch arm64" \
    architecture=arm \
    link=static threading=multi variant=release \
    install
else
  echo ">>> Boost already built at $BOOST_PREFIX (skipping)"
fi

# Modern clang rejects Boost 1.72 MPL casting (value-1) back to these unscoped
# enums in a constant expression. Give them a fixed underlying type.
echo ">>> Patching Boost numeric-conversion enums for modern clang"
for enum in int_float_mixture sign_mixture udt_builtin_mixture; do
  f="$BOOST_PREFIX/include/boost/numeric/conversion/${enum}_enum.hpp"
  if [ -f "$f" ] && ! grep -q "enum ${enum}_enum : int" "$f"; then
    perl -0pi -e "s/enum ${enum}_enum\b(?! : int)/enum ${enum}_enum : int/" "$f"
  fi
done

# --- 3. libtorrent-rasterbar 1.1.14 (static, arm64) ------------------------
if [ ! -f "$LT_PREFIX/lib/libtorrent-rasterbar.a" ]; then
  echo ">>> Building libtorrent-rasterbar 1.1.14 from source"
  cd "$WORKDIR"
  LT_DIR="libtorrent-$LIBTORRENT_COMMIT"
  if [ ! -d "$LT_DIR" ]; then
    curl -fsSL -o lt.tar.gz \
      "https://github.com/arvidn/libtorrent/archive/${LIBTORRENT_COMMIT}.tar.gz"
    tar -xzf lt.tar.gz
    rm -f lt.tar.gz
  fi
  cd "$LT_DIR"
  ./autotool.sh
  CFLAGS="-O2 -arch arm64 -I$OPENSSL_DIR/include" \
  CXXFLAGS="-O2 -arch arm64 -std=c++14 -I$OPENSSL_DIR/include -Wno-deprecated-declarations" \
  LDFLAGS="-arch arm64 -L$OPENSSL_DIR/lib" \
  PKG_CONFIG_PATH="$OPENSSL_DIR/lib/pkgconfig" \
  ./configure \
    --enable-static --disable-shared \
    --disable-deprecated-functions \
    --enable-encryption \
    --with-boost="$BOOST_PREFIX" \
    --with-boost-libdir="$BOOST_PREFIX/lib" \
    --with-openssl="$OPENSSL_DIR" \
    --prefix="$LT_PREFIX"
  make -j"$JOBS"
  make install
else
  echo ">>> libtorrent already built at $LT_PREFIX (skipping)"
fi

# --- 4. pkg-config files ---------------------------------------------------
echo ">>> Writing pkg-config files (static openssl + boost paths)"
mkdir -p "$PC_DIR"

cat > "$LT_PREFIX/lib/pkgconfig/libtorrent-rasterbar.pc" <<EOF
prefix=$LT_PREFIX
exec_prefix=\${prefix}
libdir=\${exec_prefix}/lib
includedir=\${prefix}/include
boostdir=$BOOST_PREFIX
openssldir=$OPENSSL_DIR

Name: libtorrent-rasterbar
Description: Bittorrent library.
Version: 1.1.14
Libs: -L\${libdir} -ltorrent-rasterbar -L\${boostdir}/lib -lboost_system
Libs.private: -L\${boostdir}/lib -lboost_chrono -lboost_random \${openssldir}/lib/libssl.a \${openssldir}/lib/libcrypto.a
Cflags: -I\${includedir} -I\${includedir}/libtorrent -I\${boostdir}/include -I\${openssldir}/include -DTORRENT_NO_DEPRECATE -DTORRENT_USE_OPENSSL -DBOOST_ASIO_HASH_MAP_BUCKETS=1021 -DBOOST_EXCEPTION_DISABLE -DBOOST_ASIO_ENABLE_CANCELIO
EOF

# cgo links `openssl` as a separate pkg-config package; force static archives.
cat > "$PC_DIR/openssl.pc" <<EOF
prefix=$OPENSSL_DIR
libdir=\${prefix}/lib
includedir=\${prefix}/include

Name: OpenSSL
Description: Secure Sockets Layer and cryptography libraries
Version: 3.0.0
Libs: \${libdir}/libssl.a \${libdir}/libcrypto.a
Libs.private: -lz
Cflags: -I\${includedir}
EOF

# --- 5. Local libtorrent-go with bumped go.mod -----------------------------
echo ">>> Preparing local libtorrent-go (go 1.17 for unsafe.Slice)"
LTGO_SRC="$(go env GOMODCACHE)/github.com/"'!elementum!org'"/libtorrent-go@$LIBTORRENT_GO_VERSION"
if [ ! -d "$LTGO_SRC" ]; then
  echo ">>> Fetching libtorrent-go module into cache"
  ( cd "$REPO_ROOT" && GOFLAGS=-mod=mod go mod download github.com/ElementumOrg/libtorrent-go )
fi
rm -rf "$LTGO_LOCAL"
mkdir -p "$LTGO_LOCAL"
cp -R "$LTGO_SRC/." "$LTGO_LOCAL/"
chmod -R u+w "$LTGO_LOCAL"
perl -0pi -e 's/^go 1\.14$/go 1.17/m' "$LTGO_LOCAL/go.mod"

# --- 6. Build elementum ----------------------------------------------------
echo ">>> Building elementum"
cd "$REPO_ROOT"

GIT_VERSION="${GIT_VERSION:-$(git describe --tags 2>/dev/null || echo dev)}"

# Add a temporary replace directive, build, then restore go.mod.
cp go.mod "$WORKDIR/go.mod.orig"
restore_gomod() { cp "$WORKDIR/go.mod.orig" "$REPO_ROOT/go.mod"; }
trap restore_gomod EXIT
go mod edit -replace "github.com/ElementumOrg/libtorrent-go=$LTGO_LOCAL"

mkdir -p "$OUTPUT_DIR"

PATH="$BREW_PREFIX/bin:$PATH" \
PKG_CONFIG_PATH="$PC_DIR:$LT_PREFIX/lib/pkgconfig" \
CGO_ENABLED=1 \
CGO_CXXFLAGS="-std=c++11" \
GOOS=darwin GOARCH=arm64 \
GOFLAGS=-mod=mod \
go build -tags binary,go_json \
  -ldflags="-w -X github.com/elgatito/elementum/util/ident.Version=${GIT_VERSION}" \
  -o "$OUTPUT_DIR/elementum" .

restore_gomod
trap - EXIT

# --- 7. Checksum (matches `make checksum`) ---------------------------------
shasum -b "$OUTPUT_DIR/elementum" | cut -d' ' -f1 >> "$OUTPUT_DIR/elementum"

echo ">>> Done: $OUTPUT_DIR/elementum"
file "$OUTPUT_DIR/elementum"
otool -L "$OUTPUT_DIR/elementum" | grep -i openssl && \
  echo "!!! WARNING: binary links OpenSSL dynamically" || \
  echo ">>> OpenSSL statically linked (self-contained)"
