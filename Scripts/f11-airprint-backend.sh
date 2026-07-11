#!/usr/bin/env bash
set -euo pipefail

INPUT=${1:-}
CONTENT_TYPE=${CONTENT_TYPE:-}
MAX_BYTES=${F11_AIRPRINT_MAX_BYTES:-104857600}
MAX_PAGES=${F11_AIRPRINT_MAX_PAGES:-200}
DRY_RUN=${F11_AIRPRINT_DRY_RUN:-0}
IPPTRANSFORM=${F11_AIRPRINT_IPPTRANSFORM:-$HOME/.local/ippsample/bin/ipptransform}
F11PRINT=${F11_AIRPRINT_F11PRINT:-$HOME/.local/rongta-f11/bin/f11print}
LOCK_DIR=${F11_AIRPRINT_LOCK_DIR:-$HOME/Library/Caches/F11AirPrint/print.lock}
OUTPUT_DIR=${F11_AIRPRINT_OUTPUT_DIR:-}

log() { printf 'f11-airprint: %s\n' "$*" >&2; }
die() { log "$*"; exit 1; }

[ -n "$INPUT" ] && [ -f "$INPUT" ] || die "missing job file"
case "$CONTENT_TYPE" in
  application/pdf|image/jpeg|image/png|text/plain) ;;
  *) die "unsupported document format: ${CONTENT_TYPE:-unknown}" ;;
esac

SIZE=$(stat -f %z "$INPUT")
[ "$SIZE" -gt 0 ] && [ "$SIZE" -le "$MAX_BYTES" ] || die "job size outside allowed range"

TMP=$(mktemp -d "${TMPDIR:-/tmp}/f11-airprint.XXXXXX")
NORMALIZED="$TMP/job.pdf"
LOCK_HELD=0
DELETE_INPUT=0
cleanup() {
  if [ "$DELETE_INPUT" = 1 ]; then rm -f "$INPUT"; fi
  if [ "$LOCK_HELD" = 1 ] && [ "$(cat "$LOCK_DIR/pid" 2>/dev/null || true)" = "$$" ]; then
    rm -f "$LOCK_DIR/pid"
    rmdir "$LOCK_DIR" 2>/dev/null || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT INT TERM

mkdir -p "$(dirname "$LOCK_DIR")"
for _ in $(seq 1 600); do
  if mkdir "$LOCK_DIR" 2>/dev/null; then
    printf '%s\n' "$$" > "$LOCK_DIR/pid"
    LOCK_HELD=1
    break
  fi
  owner=$(cat "$LOCK_DIR/pid" 2>/dev/null || true)
  if [ -n "$owner" ] && ! kill -0 "$owner" 2>/dev/null; then
    rm -rf "$LOCK_DIR"
    continue
  fi
  sleep 0.1
done
[ "$LOCK_HELD" = 1 ] || die "printer queue lock timeout"

[ -x "$IPPTRANSFORM" ] || die "ipptransform is unavailable"
TRANSFORM_ARGS=(-i "$CONTENT_TYPE" -m application/pdf -f "$NORMALIZED"
  -o media=na_letter_8.5x11in -o print-color-mode=monochrome
  -o printer-resolution=203dpi -o sides=one-sided)
for spec in \
  "copies:${IPP_COPIES:-}" \
  "page-ranges:${IPP_PAGE_RANGES:-}" \
  "orientation-requested:${IPP_ORIENTATION_REQUESTED:-}" \
  "print-scaling:${IPP_PRINT_SCALING:-}" \
  "number-up:${IPP_NUMBER_UP:-}"; do
  name=${spec%%:*};value=${spec#*:}
  if [ -n "$value" ]; then TRANSFORM_ARGS+=(-o "$name=$value"); fi
done
"$IPPTRANSFORM" "${TRANSFORM_ARGS[@]}" "$INPUT" >/dev/null

[ -x "$F11PRINT" ] || die "f11print is unavailable"
if [ -n "${F11_AIRPRINT_SPOOL_ROOT:-}" ]; then
  case "$INPUT" in
    "$F11_AIRPRINT_SPOOL_ROOT"/*) DELETE_INPUT=1 ;;
  esac
fi
PAGES=$("$F11PRINT" --page-count "$NORMALIZED")
[[ "$PAGES" =~ ^[0-9]+$ ]] || die "could not determine page count"
[ "$PAGES" -gt 0 ] && [ "$PAGES" -le "$MAX_PAGES" ] || die "page count outside allowed range"

[ -x "$F11PRINT" ] || die "f11print is unavailable"
if [ -z "$OUTPUT_DIR" ]; then
  OUTPUT_DIR="$TMP/output"
fi
mkdir -p "$OUTPUT_DIR"

ARGS=(--output "$OUTPUT_DIR")
export F11_RESOURCE_DIRECTORY="${F11_AIRPRINT_RESOURCE_DIRECTORY:-$(dirname "$F11PRINT")/../libexec}"
if [ "$DRY_RUN" = 1 ]; then ARGS=(--dry-run "${ARGS[@]}"); fi
"$F11PRINT" "${ARGS[@]}" "$NORMALIZED"
log "completed $PAGES page(s) from $CONTENT_TYPE"
