# VERSION is the only release-version source; build/release tools share this parser.
relay_read_version() {
  relay_value=$(cat "$1")
  printf '%s\n' "$relay_value" | LC_ALL=C grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' || {
    printf 'VERSION must use x.y.z / VERSION 必须为 x.y.z\n' >&2
    return 1
  }
  printf '%s\n' "$relay_value"
}
relay_next_version() {
  case "$2" in patch|minor|major) ;; *) printf 'Use patch, minor or major / 请使用 patch、minor 或 major\n' >&2; return 1 ;; esac
  printf '%s\n' "$1" | awk -F. -v kind="$2" '
    kind=="patch" {printf "%d.%d.%d\n", $1, $2, $3+1}
    kind=="minor" {printf "%d.%d.0\n", $1, $2+1}
    kind=="major" {printf "%d.0.0\n", $1+1}'
}
