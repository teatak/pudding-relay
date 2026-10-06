#!/bin/sh
set -eu
relay_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$relay_root"
. ./scripts/version-lib.sh
relay_kind="${1:-patch}"
case "$relay_kind" in patch|minor|major|current) ;; *) printf 'Use patch/minor/major/current / 请使用 patch/minor/major/current\n' >&2; exit 1 ;; esac
[ "$(git branch --show-current)" = main ] || { printf 'Release from main / 请在 main 发版\n' >&2; exit 1; }
[ -z "$(git status --porcelain -- . ':!VERSION')" ] || { printf 'Commit source changes before releasing / 发版前请提交源码改动\n' >&2; exit 1; }
: "${WEB_ASSETS_DIR:?Set WEB_ASSETS_DIR.}"
: "${WEB_LEGAL_DIR:?Set WEB_LEGAL_DIR.}"
command -v docker >/dev/null 2>&1
# Fetch errors stop publication; do not silently use stale release tags.
git fetch origin main --tags
git merge-base --is-ancestor origin/main HEAD || { printf 'Update main before releasing / 请先更新 main\n' >&2; exit 1; }
relay_current=$(relay_read_version VERSION)
relay_target="$relay_current"
if [ "$relay_kind" = minor ] || [ "$relay_kind" = major ]; then
  relay_target=$(relay_next_version "$relay_current" "$relay_kind")
elif [ "$relay_kind" = patch ] && git show-ref --verify --quiet "refs/tags/v$relay_current"; then
  relay_target=$(relay_next_version "$relay_current" patch)
fi
if git show-ref --verify --quiet "refs/tags/v$relay_target"; then
  printf 'Version already released / 版本已发布: %s\n' "$relay_target" >&2
  exit 1
fi
make check
make test-install
if [ "$relay_target" != "$relay_current" ]; then
  printf '%s\n' "$relay_target" > VERSION
fi
if ! git diff --quiet HEAD -- VERSION; then
  git add VERSION
  git commit -m "chore: bump version to $relay_target"
fi
# Both image tags share one manifest, then the Git release tag pins its source.
./scripts/build-image.sh --platform "${PLATFORMS:-linux/amd64,linux/arm64}" --push
git tag -a "v$relay_target" -m "Release v$relay_target"
git push --atomic origin main "v$relay_target"
printf 'Released / 已发布: v%s\n' "$relay_target"
