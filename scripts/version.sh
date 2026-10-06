#!/bin/sh
set -eu
relay_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$relay_root/scripts/version-lib.sh"
relay_current=$(relay_read_version "$relay_root/VERSION")
relay_target=$(relay_next_version "$relay_current" "${1:-patch}")
relay_version_tmp=$(mktemp "$relay_root/.VERSION.XXXXXX")
trap 'rm -f "$relay_version_tmp"' EXIT
printf '%s\n' "$relay_target" > "$relay_version_tmp"
mv "$relay_version_tmp" "$relay_root/VERSION"
printf '%s\n' "$relay_target"
