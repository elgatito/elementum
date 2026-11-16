#!/usr/bin/env bash
# Fetches the files Elementum builds into its binary from where they are maintained, the add-on and Burst:
#   config/default_settings.xml   <- plugin.video.elementum resources/settings.xml
#   ui/strings/*.po.gz            <- plugin.video.elementum resources/language/*/strings.po
#   providers/burst/providers.json, providers/burst/icons/   <- script.elementum.burst burst/providers/
#
# Where the sources come from (BUNDLE_SOURCE):
#   auto (default)  ELEMENTUM_ADDON_PATH / ELEMENTUM_BURST_PATH when set, else checkouts next to this one,
#                   else a shallow clone from GitHub
#   github          always a fresh shallow clone from GitHub, of ADDON_REF / BURST_REF (default master);
#                   what release builds use, so a release never takes a local work in progress
#   local           only the paths and the checkouts next to this one; fails when one is missing
#
# They are not in Elementum's repository (see .gitignore): run `make bundle` once before `go build` or `go test`,
# and again to pick up changes. Icons are scaled with Python's Pillow, and copied as they are without it.

set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$(pwd)

SOURCE=${BUNDLE_SOURCE:-auto}
CACHE="$ROOT/build/upstream"

# source <var> <sibling dir> <GitHub repo> <ref>: prints the absolute path of a checkout to use
source_of() {
  local var=$1 sibling=$2 repo=$3 ref=$4 path=
  if [[ $SOURCE != github ]]; then
    path=${!var:-}
    if [[ -z $path && -d "$ROOT/../$sibling" ]]; then
      path="$ROOT/../$sibling"
    fi
  fi
  if [[ -z $path ]]; then
    if [[ $SOURCE == local ]]; then
      echo "No $sibling checkout: set $var, or BUNDLE_SOURCE=github" >&2
      exit 1
    fi
    path="$CACHE/$sibling"
    rm -rf "$path"
    git clone --quiet --depth=1 --branch "$ref" "https://github.com/$repo.git" "$path" >&2
  fi
  (cd "$path" && pwd)
}

describe() { # what was bundled, for the build log
  local path=$1
  if git -C "$path" rev-parse --git-dir >/dev/null 2>&1; then
    local dirty=""
    [[ -n $(git -C "$path" status --porcelain 2>/dev/null) ]] && dirty=", with uncommitted changes"
    echo "$(git -C "$path" rev-parse --abbrev-ref HEAD) $(git -C "$path" rev-parse --short HEAD)$dirty"
  else
    echo "not a git checkout"
  fi
}

ADDON=$(source_of ELEMENTUM_ADDON_PATH plugin.video.elementum elgatito/plugin.video.elementum "${ADDON_REF:-master}")
BURST=$(source_of ELEMENTUM_BURST_PATH script.elementum.burst elgatito/script.elementum.burst "${BURST_REF:-master}")
echo "Bundling the add-on from $ADDON ($(describe "$ADDON"))"
echo "Bundling Burst from $BURST ($(describe "$BURST"))"

export ELEMENTUM_ADDON_PATH=$ADDON ELEMENTUM_BURST_PATH=$BURST
go generate ./config ./ui ./providers/burst

