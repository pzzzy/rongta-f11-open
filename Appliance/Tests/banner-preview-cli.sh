#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
BIN="$TMP/bannerprint"
PNG="$TMP/banner.png"
REPORT="$TMP/report.json"

cd "$ROOT"
go build -trimpath -o "$BIN" ./cmd/bannerprint
"$BIN" --preview --preview-png "$PNG" '*' >"$REPORT"

python3 - "$PNG" "$REPORT" <<'PY'
import json
import struct
import sys
from pathlib import Path

png = Path(sys.argv[1])
report = json.loads(Path(sys.argv[2]).read_text())
assert report["ok"] is True
assert report["preview"] is True
assert report["submitted"] is False
assert report["lines"] == ["*"], report["lines"]
assert png.is_file() and png.stat().st_size > 8
with png.open("rb") as f:
    assert f.read(8) == b"\x89PNG\r\n\x1a\n"
    length = struct.unpack(">I", f.read(4))[0]
    assert f.read(4) == b"IHDR"
    data = f.read(length)
width, height = struct.unpack(">II", data[:8])
assert width == 1664, width
assert height == report["rows"] and height > 0, (height, report["rows"])
assert png.stat().st_mode & 0o777 == 0o600
print("banner preview CLI integration: PASS")
PY

if "$BIN" --preview-png "$TMP/rejected.png" HELLO >/dev/null 2>&1; then
  echo "preview PNG was accepted without --preview" >&2
  exit 1
fi

test ! -e "$TMP/rejected.png"
