#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
PLIST="$HOME/Library/LaunchAgents/com.pzzzy.f11-airprint.plist"
STATE="$HOME/Library/Application Support/F11AirPrint"
LABEL="gui/$(id -u)/com.pzzzy.f11-airprint"
TMP=$(mktemp -d)
restart_agent() {
  launchctl bootout "$LABEL" >/dev/null 2>&1 || true
  for _ in $(seq 1 50);do
    if ! launchctl print "$LABEL" >/dev/null 2>&1;then break;fi
    sleep .1
  done
  for _ in $(seq 1 20);do
    if launchctl bootstrap "gui/$(id -u)" "$PLIST" >/dev/null 2>&1;then
      launchctl kickstart -k "$LABEL" >/dev/null
      return 0
    fi
    sleep .25
  done
  return 1
}
set_dry_run() {
  python3 - "$PLIST" "$1" <<'PY'
import plistlib,sys
p=sys.argv[1];enable=sys.argv[2]=='1'
with open(p,'rb') as f:d=plistlib.load(f)
a=[x for x in d['ProgramArguments'] if x!='F11_AIRPRINT_DRY_RUN=1']
if enable:a.insert(-1,'F11_AIRPRINT_DRY_RUN=1')
d['ProgramArguments']=a
with open(p,'wb') as f:plistlib.dump(d,f,sort_keys=False)
PY
}
restore() {
  set_dry_run 0
  restart_agent || true
  rm -rf "$TMP" "$STATE/output"
}
trap restore EXIT INT TERM

set_dry_run 1
restart_agent
rm -rf "$STATE/output";mkdir -p "$STATE/output"
swift "$ROOT/Tests/make-test-pdf.swift" "$TMP/two-pages.pdf" 2
ipptool -q -d filename="$TMP/two-pages.pdf" ipp://localhost:8631/ipp/print "$ROOT/Tests/ipp/print-job.test"
for _ in $(seq 1 100);do
  [ "$(find "$STATE/output" -name '*.f11' | wc -l | tr -d ' ')" = 2 ] && break
  sleep .2
done
[ "$(find "$STATE/output" -name '*.f11' | wc -l | tr -d ' ')" = 2 ]
echo "PASS two-page IPP job produced two validated streams"
