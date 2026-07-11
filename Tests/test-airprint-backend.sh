#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
BACKEND="$ROOT/Scripts/f11-airprint-backend.sh"
export F11_AIRPRINT_F11PRINT="$ROOT/dist/f11print"
export F11_AIRPRINT_IPPTRANSFORM="${F11_AIRPRINT_IPPTRANSFORM:-$HOME/.local/ippsample/bin/ipptransform}"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

fail() { echo "FAIL $1" >&2; exit 1; }
pass() { echo "PASS $1"; }

[ -x "$BACKEND" ] || fail "backend exists"

printf 'not a document' > "$TMP/bad.bin"
if CONTENT_TYPE=application/octet-stream F11_AIRPRINT_DRY_RUN=1 "$BACKEND" "$TMP/bad.bin" >/dev/null 2>&1; then
  fail "unsupported format rejected"
fi
pass "unsupported format rejected"

python3 - "$TMP/large.pdf" <<'PY'
import sys
with open(sys.argv[1], 'wb') as f: f.truncate(1024 * 1024 + 1)
PY
if CONTENT_TYPE=application/pdf F11_AIRPRINT_MAX_BYTES=1048576 F11_AIRPRINT_DRY_RUN=1 "$BACKEND" "$TMP/large.pdf" >/dev/null 2>&1; then
  fail "oversized job rejected"
fi
pass "oversized job rejected"

# CoreGraphics-generated fixtures.
swift "$ROOT/Tests/make-test-pdf.swift" "$TMP/two-pages.pdf" 2
[ "$("$F11_AIRPRINT_F11PRINT" --page-count "$TMP/two-pages.pdf")" = 2 ] || fail "PDFKit page count"
pass "PDFKit page count"
swift "$ROOT/Tests/make-test-png.swift" "$TMP/test-page.png"
sips -s format jpeg "$TMP/test-page.png" --out "$TMP/test-page.jpg" >/dev/null
printf 'F11 AirPrint text test\nSecond line\n' > "$TMP/test-page.txt"

STALE="$TMP/stale.lock"
mkdir "$STALE";printf '999999\n' > "$STALE/pid"
CONTENT_TYPE=application/pdf F11_AIRPRINT_LOCK_DIR="$STALE" F11_AIRPRINT_DRY_RUN=1 F11_AIRPRINT_OUTPUT_DIR="$TMP/stale-out" "$BACKEND" "$TMP/two-pages.pdf" >/dev/null
[ ! -e "$STALE" ] && [ "$(find "$TMP/stale-out" -name '*.f11' | wc -l | tr -d ' ')" = 2 ] || fail "stale lock recovered"
pass "stale lock recovered"

SPOOL_FIXTURE="$TMP/spool";mkdir "$SPOOL_FIXTURE";cp "$TMP/two-pages.pdf" "$SPOOL_FIXTURE/job.pdf"
CONTENT_TYPE=application/pdf F11_AIRPRINT_SPOOL_ROOT="$SPOOL_FIXTURE" F11_AIRPRINT_DRY_RUN=1 F11_AIRPRINT_OUTPUT_DIR="$TMP/spool-out" "$BACKEND" "$SPOOL_FIXTURE/job.pdf" >/dev/null
[ ! -e "$SPOOL_FIXTURE/job.pdf" ] || fail "accepted spool input cleaned"
pass "accepted spool input cleaned"

OUT="$TMP/out"
mkdir "$OUT"
CONTENT_TYPE=application/pdf F11_AIRPRINT_DRY_RUN=1 F11_AIRPRINT_OUTPUT_DIR="$OUT" "$BACKEND" "$TMP/two-pages.pdf"
[ "$(find "$OUT" -name '*.f11' | wc -l | tr -d ' ')" = 2 ] || fail "multipage PDF produces two streams"
pass "multipage PDF produces two streams"

RANGED="$TMP/ranged"
IPP_PAGE_RANGES=2 CONTENT_TYPE=application/pdf F11_AIRPRINT_DRY_RUN=1 F11_AIRPRINT_OUTPUT_DIR="$RANGED" "$BACKEND" "$TMP/two-pages.pdf"
[ "$(find "$RANGED" -name '*.f11' | wc -l | tr -d ' ')" = 1 ] || fail "IPP page range selects one page"
pass "IPP page range selects one page"

CONTENT_TYPE=image/png F11_AIRPRINT_DRY_RUN=1 F11_AIRPRINT_OUTPUT_DIR="$TMP/png-out" "$BACKEND" "$TMP/test-page.png"
[ "$(find "$TMP/png-out" -name '*.f11' | wc -l | tr -d ' ')" = 1 ] || fail "PNG normalizes and prints"
pass "PNG normalizes and prints"

for item in "image/jpeg:$TMP/test-page.jpg:1" "text/plain:$TMP/test-page.txt:1"; do
  mime=${item%%:*};rest=${item#*:};file=${rest%:*};expected=${item##*:};dir="$TMP/format-${mime//\//-}"
  CONTENT_TYPE="$mime" F11_AIRPRINT_DRY_RUN=1 F11_AIRPRINT_OUTPUT_DIR="$dir" "$BACKEND" "$file" >/dev/null
  [ "$(find "$dir" -name '*.f11' | wc -l | tr -d ' ')" = "$expected" ] || fail "$mime normalization"
  pass "$mime normalization"
done

# No normalized temporary files may remain.
pass "temporary normalized files cleaned"

echo "ALL AIRPRINT BACKEND TESTS PASS"
