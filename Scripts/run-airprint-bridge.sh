#!/usr/bin/env bash
set -euo pipefail
umask 077

PREFIX=${F11_AIRPRINT_PREFIX:-$HOME/.local/rongta-f11}
IPPSAMPLE=${F11_AIRPRINT_IPPSAMPLE:-$HOME/.local/ippsample}
PORT=${F11_AIRPRINT_PORT:-8631}
STATE=${F11_AIRPRINT_STATE:-$HOME/Library/Application Support/F11AirPrint}
DRY_RUN=${F11_AIRPRINT_DRY_RUN:-0}

# Do not propagate unrelated login/session credentials into the network-facing service.
unset CODEX_LB_API_KEY OPENAI_API_KEY ANTHROPIC_API_KEY GITHUB_TOKEN GH_TOKEN AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY

SERVER="$IPPSAMPLE/sbin/ippserver"
BACKEND="$PREFIX/libexec/f11-airprint-backend"
ATTRS="$PREFIX/share/f11-airprint/printer.attrs"
SPOOL="$STATE/spool"
OUTPUT="$STATE/output"
mkdir -p "$SPOOL" "$OUTPUT" "$STATE/logs"
chmod 700 "$STATE" "$SPOOL" "$OUTPUT" "$STATE/logs"

[ -x "$SERVER" ] || { echo "missing ippserver: $SERVER" >&2; exit 1; }
[ -x "$BACKEND" ] || { echo "missing backend: $BACKEND" >&2; exit 1; }
[ -f "$ATTRS" ] || { echo "missing attributes: $ATTRS" >&2; exit 1; }

export F11_AIRPRINT_IPPTRANSFORM="$IPPSAMPLE/bin/ipptransform"
export F11_AIRPRINT_F11PRINT="$PREFIX/bin/f11print"
export F11_AIRPRINT_SPOOL_ROOT="$SPOOL"
export F11_AIRPRINT_DRY_RUN="$DRY_RUN"
if [ "$DRY_RUN" = 1 ]; then export F11_AIRPRINT_OUTPUT_DIR="$OUTPUT"; fi

exec "$SERVER" \
  --no-web-forms \
  -a "$ATTRS" \
  -c "$BACKEND" \
  -d "$SPOOL" \
  -f application/pdf,image/jpeg,image/png,text/plain \
  -F application/pdf \
  -M Rongta \
  -m "F11 Open AirPrint Bridge" \
  -l "USB printer on $(scutil --get ComputerName 2>/dev/null || hostname)" \
  -p "$PORT" \
  -r _print,_universal \
  -s 4,0 \
  "Rongta F11"
