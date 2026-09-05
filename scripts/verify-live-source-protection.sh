#!/bin/sh
# Additional process-level evidence. Live before/after equality remains a
# separate mandatory result; write enforcement cannot turn that failure green.
set -eu
umask 077

controls_only=0
evidence_dir=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --controls-only) controls_only=1; shift ;;
    --evidence-dir)
      [ "$#" -ge 2 ] || { echo "--evidence-dir requires a new directory path" >&2; exit 2; }
      evidence_dir=$2
      shift 2
      ;;
    *) echo "usage: $0 [--controls-only] [--evidence-dir NEW_DIRECTORY]" >&2; exit 2 ;;
  esac
done

if [ "$(uname -s)" != "Darwin" ] || ! command -v sandbox-exec >/dev/null 2>&1 || ! command -v cc >/dev/null 2>&1; then
  echo "source-protection verification requires macOS, sandbox-exec, and a C compiler" >&2
  exit 2
fi

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
work_root=""
canary_root=""
run_pid=""
cleanup() {
  result=$?
  trap - EXIT HUP INT TERM
  if [ -n "$run_pid" ]; then
    kill "$run_pid" 2>/dev/null || true
    wait "$run_pid" 2>/dev/null || true
  fi
  if [ -n "$evidence_dir" ] && [ -n "$work_root" ] && [ -d "$work_root/evidence" ]; then
    # Retain only selected evidence; raw SQLite copies and generated data never leave TMPDIR.
    if ! cp -R "$work_root/evidence/." "$evidence_dir/"; then
      echo "failed to retain verification evidence" >&2
      result=1
    else
      echo "Private evidence: $evidence_dir"
    fi
  fi
  if [ -n "$work_root" ]; then
    case "$work_root" in
      /private/tmp/skuggsja-source-protection.*) rm -rf -- "$work_root" || result=1 ;;
      *) echo "refusing unexpected work-root cleanup" >&2; result=1 ;;
    esac
  fi
  if [ -n "$canary_root" ]; then
    case "$canary_root" in
      /private/tmp/skuggsja-source-canary.*) rm -rf -- "$canary_root" || result=1 ;;
      *) echo "refusing unexpected canary cleanup" >&2; result=1 ;;
    esac
  fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if [ -n "$evidence_dir" ]; then
  # mkdir must fail for any existing path; never change another directory's permissions.
  mkdir -m 700 -- "$evidence_dir"
  evidence_dir=$(CDPATH= cd -- "$evidence_dir" && pwd -P)
fi
work_root=$(mktemp -d /private/tmp/skuggsja-source-protection.XXXXXX)
canary_root=$(mktemp -d /private/tmp/skuggsja-source-canary.XXXXXX)
mkdir -m 700 "$work_root/bin" "$work_root/tmp" "$work_root/evidence" \
  "$work_root/positive-unconfined" "$work_root/positive-denied" \
  "$canary_root/unconfined" "$canary_root/denied"

CGO_ENABLED=0 go -C "$repo_dir" build -trimpath -o "$work_root/bin/source-write-probe" ./scripts/source-write-probe
CGO_ENABLED=0 go -C "$repo_dir" build -trimpath -o "$work_root/bin/network-probe" ./scripts/network-probe
CGO_ENABLED=0 go -C "$repo_dir" test -c -trimpath -o "$work_root/bin/app.test" ./internal/app
cc -dynamiclib -Os -Wall -Wextra -Werror \
  -o "$work_root/bin/network-guard.dylib" "$repo_dir/scripts/network_guard_darwin.c"

profile="$work_root/evidence/source-protection.sb"
cat "$repo_dir/scripts/macos-network-deny.sb" >"$profile"
cat >>"$profile" <<'PROFILE'

; All original paths remain readable. Only this invocation's new private work
; directory and /dev/null can be written; HOME and harness roots are preserved.
(deny file-write*)
(allow file-write* (subpath (param "WRITABLE_ROOT")))
(allow file-write* (literal "/dev/null"))
PROFILE

shasum -a 256 "$work_root/bin/app.test" "$work_root/bin/source-write-probe" \
  "$work_root/bin/network-probe" "$work_root/bin/network-guard.dylib" "$profile" \
  "$repo_dir/scripts/verify-live-source-protection.sh" "$repo_dir/scripts/source-write-probe/main.go" \
  >"$work_root/evidence/artifact-sha256.txt"
echo "profile_parameter_writable_root=$work_root" >"$work_root/evidence/execution.txt"

control_log="$work_root/evidence/controls.log"
"$work_root/bin/source-write-probe" prepare "$canary_root/unconfined"
"$work_root/bin/source-write-probe" prepare "$canary_root/denied"
if ! "$work_root/bin/source-write-probe" unconfined "$canary_root/unconfined" "$work_root/positive-unconfined" >"$control_log" 2>&1; then
  cat "$control_log" >&2
  exit 1
fi
if ! sandbox-exec -D "WRITABLE_ROOT=$work_root" -f "$profile" \
  "$work_root/bin/source-write-probe" denied "$canary_root/denied" "$work_root/positive-denied" >>"$control_log" 2>&1; then
  cat "$control_log" >&2
  exit 1
fi

network_control="$work_root/evidence/network-control.log"
: >"$network_control"
if ! sandbox-exec -D "WRITABLE_ROOT=$work_root" -f "$profile" /usr/bin/env \
  DYLD_INSERT_LIBRARIES="$work_root/bin/network-guard.dylib" \
  SKUGGSJA_NETWORK_ATTEMPT_LOG="$network_control" \
  "$work_root/bin/network-probe"; then
  echo "network negative control unexpectedly connected" >&2
  exit 1
fi
if ! grep -Fx 'network-guard-active' "$network_control" >/dev/null || \
   ! grep -Fx 'external-connect-attempt' "$network_control" >/dev/null; then
  echo "network negative control was not observed by the guard" >&2
  exit 1
fi
cat "$control_log"
echo "network_control guard_active=true external_attempt_observed=true connection_denied=true"

summary="$work_root/evidence/summary.txt"
if [ "$controls_only" -eq 1 ]; then
  echo "PASS: synthetic source-write and network denial controls; real-data run not performed" | tee "$summary"
  exit 0
fi

attempt_log="$work_root/evidence/network-attempts.log"
test_log="$work_root/evidence/live-source-test.log"
: >"$attempt_log"
date -u '+started_utc=%Y-%m-%dT%H:%M:%SZ' >>"$work_root/evidence/execution.txt"
sandbox-exec -D "WRITABLE_ROOT=$work_root" -f "$profile" /usr/bin/env \
  TMPDIR="$work_root/tmp" \
  SKUGGSJA_VERIFY_REAL_DATA=1 \
  DYLD_INSERT_LIBRARIES="$work_root/bin/network-guard.dylib" \
  SKUGGSJA_NETWORK_ATTEMPT_LOG="$attempt_log" \
  "$work_root/bin/app.test" -test.run '^TestRealDataFullRunLeavesSourcesUnchanged$' -test.v -test.timeout 6m \
  >"$test_log" 2>&1 &
run_pid=$!
echo "pid=$run_pid" >>"$work_root/evidence/execution.txt"
test_status=0
wait "$run_pid" || test_status=$?
run_pid=""
date -u '+finished_utc=%Y-%m-%dT%H:%M:%SZ' >>"$work_root/evidence/execution.txt"
echo "test_exit=$test_status" >>"$work_root/evidence/execution.txt"
cat "$test_log"

network_status=0
guard_active=false
if grep -Fx 'network-guard-active' "$attempt_log" >/dev/null; then
  guard_active=true
else
  network_status=1
fi
external_attempts=$(grep -Fc 'external-connect-attempt' "$attempt_log" || true)
if [ "$external_attempts" -ne 0 ]; then
  network_status=1
fi
live_result=failed
live_status=1
if [ "$test_status" -eq 0 ] && \
   grep -F -- '--- PASS: TestRealDataFullRunLeavesSourcesUnchanged' "$test_log" >/dev/null && \
   grep -E 'outer_source_audit .*verified=true' "$test_log" >/dev/null && \
   grep -E 'internal_source_audit .*verified=true' "$test_log" >/dev/null; then
  live_result=passed
  live_status=0
fi
{
  echo "source_write_enforcement=calibrated source_writes=denied_by_policy writable_paths=private_workspace_and_dev_null"
  echo "source_write_attempts=not_independently_traced"
  echo "network_guard_active=$guard_active observed_external_attempts=$external_attempts network_check_exit=$network_status"
  echo "strict_live_manifest_result=$live_result live_test_exit=$test_status"
} | tee "$summary"
if [ "$live_status" -ne 0 ] || [ "$network_status" -ne 0 ]; then
  exit 1
fi
