#!/bin/sh
set -eu

relay_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
: "${WEB_ASSETS_DIR:?Set WEB_ASSETS_DIR to the compiled Pudding browser build.}"
: "${WEB_LEGAL_DIR:?Set WEB_LEGAL_DIR to the matching Pudding license/notice directory.}"
IMAGE="${IMAGE:-teatak/pudding-relay:latest}"
. "$relay_root/scripts/version-lib.sh"
relay_version=$(relay_read_version "$relay_root/VERSION")
case "${IMAGE##*/}" in
  *@*) printf 'Build IMAGE must use a repository/tag / 构建 IMAGE 请使用仓库与标签\n' >&2; exit 1 ;;
  *:*) relay_repository="${IMAGE%:*}" ;;
  *) relay_repository="$IMAGE" ;;
esac
[ -f "$WEB_ASSETS_DIR/index.html" ] || { printf 'Missing browser index.html\n' >&2; exit 1; }
grep -q '__PUDDING_REMOTE_BASE__' "$WEB_ASSETS_DIR/index.html" || { printf 'Not a Pudding browser build\n' >&2; exit 1; }
[ -f "$WEB_LEGAL_DIR/PUDDING-LICENSE.txt" ] && [ -f "$WEB_LEGAL_DIR/THIRD_PARTY_NOTICES.txt" ] || { printf 'Missing browser license/notices\n' >&2; exit 1; }
node "$relay_root/scripts/validate-browser-build.cjs" "$WEB_ASSETS_DIR"
relay_context=$(mktemp -d "${TMPDIR:-/tmp}/pudding-relay-browser.XXXXXX")
trap 'rm -rf "$relay_context"' EXIT
trap 'exit 1' HUP INT TERM
cp -R "$WEB_ASSETS_DIR/." "$relay_context/"
mkdir "$relay_context/licenses"
cp -R "$WEB_LEGAL_DIR/." "$relay_context/licenses/"
# Ship compiled UI and notices only; source maps are not part of the public image.
find "$relay_context" -type f -name '*.map' -delete
relay_commit=$(git -C "$relay_root" rev-parse HEAD)
docker buildx build --build-context "browser=$relay_context" --build-arg "COMMIT=$relay_commit" --tag "$IMAGE" --tag "$relay_repository:$relay_version" "$@" "$relay_root"
