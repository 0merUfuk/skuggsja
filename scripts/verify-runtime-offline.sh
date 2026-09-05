#!/bin/sh
set -eu

if [ "$(uname -s)" != "Darwin" ] || ! command -v cc >/dev/null 2>&1; then
  echo "runtime isolation check requires macOS and a C compiler" >&2
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

mkdir -p "$verify_root/bin" "$verify_root/output" "$verify_root/missing" "$verify_root/sources"
CGO_ENABLED=0 go -C "$repo_dir" build -trimpath -o "$verify_root/bin/skuggsja" ./cmd/skuggsja
CGO_ENABLED=0 go -C "$repo_dir" build -trimpath -o "$verify_root/bin/network-probe" ./scripts/network-probe
CGO_ENABLED=0 go -C "$repo_dir" build -trimpath -o "$verify_root/bin/verification-fixtures" ./scripts/verification-fixtures
cc -dynamiclib -Os -Wall -Wextra -Werror \
  -o "$verify_root/bin/network-guard.dylib" "$repo_dir/scripts/network_guard_darwin.c"

"$verify_root/bin/verification-fixtures" \
  "$verify_root/sources/hermes/state.db" \
  "$verify_root/sources/cursor/state.vscdb"

attempt_log="$verify_root/network-attempts.log"
: >"$attempt_log"
if ! env \
  DYLD_INSERT_LIBRARIES="$verify_root/bin/network-guard.dylib" \
  SKUGGSJA_NETWORK_ATTEMPT_LOG="$attempt_log" \
  "$verify_root/bin/network-probe"; then
  echo "network-isolation control failed: the remote probe connected" >&2
  exit 1
fi
if ! grep -Fx "network-guard-active" "$attempt_log" >/dev/null || \
   ! grep -Fx "external-connect-attempt" "$attempt_log" >/dev/null; then
  echo "network-isolation control failed: the remote probe was not observed" >&2
  exit 1
fi
: >"$attempt_log"

server_log="$verify_root/server.log"

env \
  SKUGGSJA_OUTPUT_DIRECTORY="$verify_root/output" \
  SKUGGSJA_CLAUDE_PROJECTS="$repo_dir/testdata/claude" \
  SKUGGSJA_CLAUDE_EXTRA_HOMES="" \
  SKUGGSJA_CLAUDE_HISTORY="$verify_root/missing/claude-history.jsonl" \
  SKUGGSJA_CLAUDE_STATS="$verify_root/missing/claude-stats.json" \
  SKUGGSJA_CLAUDE_GLOBAL_STATE="$verify_root/missing/claude-global.json" \
  SKUGGSJA_CLAUDE_DESKTOP_SESSIONS="$verify_root/missing/claude-desktop" \
  SKUGGSJA_CLAUDE_CODE_SESSIONS="$verify_root/missing/claude-code" \
  SKUGGSJA_CODEX_SESSIONS="$repo_dir/testdata/codex/sessions" \
  SKUGGSJA_CODEX_ARCHIVED="$verify_root/missing/codex-archive" \
  SKUGGSJA_CODEX_RECOVERY="$verify_root/missing/codex-recovery" \
  SKUGGSJA_CODEX_HISTORY="$verify_root/missing/codex-history.jsonl" \
  SKUGGSJA_CODEX_SESSION_INDEX="$verify_root/missing/codex-session-index.jsonl" \
  SKUGGSJA_CODEX_EXTERNAL_IMPORTS="$verify_root/missing/codex-imports.json" \
  SKUGGSJA_CODEX_STATE_DATABASE="$verify_root/missing/codex-state.sqlite" \
  SKUGGSJA_CODEX_CATALOG_DATABASE="$verify_root/missing/codex-catalog.sqlite" \
  SKUGGSJA_CODEX_THREAD_HISTORY_DATABASE="$verify_root/missing/codex-thread-history.sqlite" \
  SKUGGSJA_HERMES_DATABASE="$verify_root/sources/hermes/state.db" \
  SKUGGSJA_CURSOR_DATABASE="$verify_root/sources/cursor/state.vscdb" \
  sandbox-exec -f "$repo_dir/scripts/macos-network-deny.sb" \
  /usr/bin/env \
  DYLD_INSERT_LIBRARIES="$verify_root/bin/network-guard.dylib" \
  SKUGGSJA_NETWORK_ATTEMPT_LOG="$attempt_log" \
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

if ! grep -Fx "network-guard-active" "$attempt_log" >/dev/null; then
  echo "runtime network guard did not load" >&2
  exit 1
fi
if grep -Fx "external-connect-attempt" "$attempt_log" >/dev/null; then
  echo "runtime attempted an external connection" >&2
  exit 1
fi

grep -F "Content-Security-Policy: default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'none'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'" "$header_file" >/dev/null

kill "$server_pid"
wait "$server_pid" || true
server_pid=""
cat "$server_log"

echo "PASS: generation and localhost viewing completed with zero observed external connect attempts"
