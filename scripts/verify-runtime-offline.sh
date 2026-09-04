#!/bin/sh
set -eu

if [ "$(uname -s)" != "Darwin" ] || ! command -v sandbox-exec >/dev/null 2>&1; then
  echo "runtime isolation check requires macOS sandbox-exec" >&2
  exit 2
fi

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
verify_root=$(mktemp -d "${TMPDIR:-/tmp}/skuggsja-offline.XXXXXX")
server_pid=""

cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  case "$verify_root" in
    "${TMPDIR:-/tmp}"/skuggsja-offline.*) rm -rf -- "$verify_root" ;;
    *) echo "refusing to clean unexpected temporary path: $verify_root" >&2 ;;
  esac
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$verify_root/bin" "$verify_root/home" "$verify_root/missing"
CGO_ENABLED=0 go -C "$repo_dir" build -trimpath -o "$verify_root/bin/skuggsja" ./cmd/skuggsja

if sandbox-exec -f "$repo_dir/scripts/macos-network-deny.sb" \
  /usr/bin/curl --silent --show-error --connect-timeout 2 --max-time 3 https://example.com \
  >/dev/null 2>&1; then
  echo "network-isolation control failed: a remote request succeeded" >&2
  exit 1
fi

server_log="$verify_root/server.log"

env \
  HOME="$verify_root/home" \
  SKUGGSJA_CLAUDE_PROJECTS="$repo_dir/testdata/claude" \
  SKUGGSJA_CODEX_SESSIONS="$repo_dir/testdata/codex/sessions" \
  SKUGGSJA_CODEX_ARCHIVED="$verify_root/missing/codex-archive" \
  SKUGGSJA_HERMES_DATABASE="$verify_root/missing/hermes.db" \
  SKUGGSJA_CURSOR_DATABASE="$verify_root/missing/cursor.vscdb" \
  sandbox-exec -f "$repo_dir/scripts/macos-network-deny.sb" \
  "$verify_root/bin/skuggsja" --no-open --port 0 >"$server_log" 2>&1 &
server_pid=$!

local_url=""
attempt=0
while [ "$attempt" -lt 200 ]; do
  if ! kill -0 "$server_pid" 2>/dev/null; then
    cat "$server_log" >&2
    wait "$server_pid" || true
    server_pid=""
    echo "sandboxed server stopped before becoming ready" >&2
    exit 1
  fi
  local_url=$(sed -n 's/^.*→ \(http:\/\/127\.0\.0\.1:[0-9][0-9]*\)$/\1/p' "$server_log" | tail -n 1)
  if [ -n "$local_url" ]; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 0.05
done

if [ -z "$local_url" ]; then
  cat "$server_log" >&2
  echo "sandboxed server did not report a loopback URL" >&2
  exit 1
fi

header_file="$verify_root/headers.txt"
/usr/bin/curl --fail --silent --show-error "$local_url/healthz" >/dev/null
/usr/bin/curl --fail --silent --show-error --dump-header "$header_file" "$local_url/" >/dev/null
/usr/bin/curl --fail --silent --show-error "$local_url/styles.css" >/dev/null
/usr/bin/curl --fail --silent --show-error "$local_url/app.js" >/dev/null
/usr/bin/curl --fail --silent --show-error "$local_url/api/rewind" >/dev/null

grep -F "Content-Security-Policy: default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'none'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'" "$header_file" >/dev/null

kill "$server_pid"
wait "$server_pid" || true
server_pid=""
cat "$server_log"

echo "PASS: generation and localhost viewing completed with remote networking denied"
