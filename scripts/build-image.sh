#!/bin/sh
set -eu

relay_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
IMAGE="${IMAGE:-teatak/pudding-relay:latest}"
. "$relay_root/scripts/version-lib.sh"
relay_version=$(relay_read_version "$relay_root/VERSION")
case "${IMAGE##*/}" in
  *@*) printf 'Build IMAGE must use a repository/tag / 构建 IMAGE 请使用仓库与标签\n' >&2; exit 1 ;;
  *:*) relay_repository="${IMAGE%:*}" ;;
  *) relay_repository="$IMAGE" ;;
esac
relay_commit=$(git -C "$relay_root" rev-parse HEAD)
docker buildx build --build-arg "COMMIT=$relay_commit" --tag "$IMAGE" --tag "$relay_repository:$relay_version" "$@" "$relay_root"
