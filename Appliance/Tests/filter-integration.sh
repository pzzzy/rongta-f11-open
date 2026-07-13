#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
for cmd in go qpdf gs; do
  if ! command -v "$cmd" >/dev/null; then
    [[ ${F11_ALLOW_TEST_SKIP:-0} == 1 ]] && { echo "SKIP: missing $cmd"; exit 0; }
    echo "ERROR: missing integration dependency: $cmd" >&2
    exit 1
  fi
done
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cd "$ROOT"
go build -o "$TMP/f11d-real" ./cmd/f11d
python3 - "$TMP/clean.pdf" <<'PY'
from pathlib import Path
import sys
pages=[]
for n in range(1,4): pages.append(f"BT /F1 24 Tf 72 720 Td (F11 PAGE {n}) Tj ET".encode())
objs=[b'<< /Type /Catalog /Pages 2 0 R >>']
kids=' '.join(f'{3+i*2} 0 R' for i in range(len(pages))).encode(); objs.append(b'<< /Type /Pages /Kids ['+kids+b'] /Count 3 >>')
font_id=9
for i,s in enumerate(pages):
 content=4+i*2; objs.append(f'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 {font_id} 0 R >> >> /Contents {content} 0 R >>'.encode()); objs.append(f'<< /Length {len(s)} >>\nstream\n'.encode()+s+b'\nendstream')
objs.append(b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>')
out=bytearray(b'%PDF-1.4\n'); offsets=[0]
for i,obj in enumerate(objs,1): offsets.append(len(out)); out+=f'{i} 0 obj\n'.encode()+obj+b'\nendobj\n'
xref=len(out); out+=f'xref\n0 {len(objs)+1}\n0000000000 65535 f \n'.encode()
for off in offsets[1:]: out+=f'{off:010d} 00000 n \n'.encode()
out+=f'trailer << /Size {len(objs)+1} /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n'.encode(); Path(sys.argv[1]).write_bytes(out)
PY
qpdf --check "$TMP/clean.pdf" >/dev/null
python3 - "$TMP/short.pdf" <<'PY'
from pathlib import Path
import sys
# 590 x 90.8 points maps to 1664 x 256 pixels at 203 dpi.
s=b"0 0 0 rg 220 5 150 4 re f 250 40 90 30 re S 210 82 170 5 re f"
objs=[b'<< /Type /Catalog /Pages 2 0 R >>',b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 590 90.8] /Contents 4 0 R >>',f'<< /Length {len(s)} >>\nstream\n'.encode()+s+b'\nendstream']
out=bytearray(b'%PDF-1.4\n'); offsets=[0]
for i,obj in enumerate(objs,1): offsets.append(len(out)); out+=f'{i} 0 obj\n'.encode()+obj+b'\nendobj\n'
xref=len(out); out+=b'xref\n0 5\n0000000000 65535 f \n'
for off in offsets[1:]: out+=f'{off:010d} 00000 n \n'.encode()
out+=f'trailer << /Size 5 /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n'.encode(); Path(sys.argv[1]).write_bytes(out)
PY
qpdf --check "$TMP/short.pdf" >/dev/null
python3 - "$TMP/portrait.pdf" <<'PY'
from pathlib import Path
import sys
s=b"0 0 0 rg 20 20 248 392 re S"
objs=[b'<< /Type /Catalog /Pages 2 0 R >>',b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 288 432] /Contents 4 0 R >>',f'<< /Length {len(s)} >>\nstream\n'.encode()+s+b'\nendstream']
out=bytearray(b'%PDF-1.4\n'); offsets=[0]
for i,obj in enumerate(objs,1): offsets.append(len(out)); out+=f'{i} 0 obj\n'.encode()+obj+b'\nendobj\n'
xref=len(out); out+=b'xref\n0 5\n0000000000 65535 f \n'
for off in offsets[1:]: out+=f'{off:010d} 00000 n \n'.encode()
out+=f'trailer << /Size 5 /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n'.encode(); Path(sys.argv[1]).write_bytes(out)
PY
qpdf --check "$TMP/portrait.pdf" >/dev/null
[[ $("$ROOT/scripts/pdf-page-height.py" --dimensions "$TMP/portrait.pdf" 1) == '812 1218' ]]
[[ $("$ROOT/scripts/pdf-page-height.py" "$TMP/portrait.pdf" 1) == '1218' ]]
python3 - "$TMP/portrait.pdf" "$TMP/rotated.pdf" <<'PY'
from pathlib import Path
import sys
b=Path(sys.argv[1]).read_bytes()
old=b'/MediaBox [0 0 288 432] /Contents'
new=b'/MediaBox [0 0 288 432] /Rotate 90 /Contents'
assert b.count(old)==1
# Rebuild with qpdf after this length-changing, deliberately invalid-xref edit.
Path(sys.argv[2]).write_bytes(b.replace(old,new))
PY
qpdf --warning-exit-0 "$TMP/rotated.pdf" "$TMP/rotated-fixed.pdf"
qpdf --check "$TMP/rotated-fixed.pdf" >/dev/null
[[ $("$ROOT/scripts/pdf-page-height.py" --dimensions "$TMP/rotated-fixed.pdf" 1) == '1218 812' ]]
mkdir "$TMP/spool"
F11D="$TMP/f11d-real" F11_SPOOL="$TMP/spool" F11_OUTPUT_DIR="$TMP/spool" PAGE_HEIGHT="$ROOT/scripts/pdf-page-height.py" MEDIA_CANVAS="$ROOT/scripts/media-canvas.py" \
  "$ROOT/cups/pdftof11" 99 tester clean 2 '' "$TMP/clean.pdf" >"$TMP/output.f11" 2>"$TMP/filter.log"
python3 - "$TMP/output.f11" "$TMP/jobs" <<'PY'
from pathlib import Path
import struct,sys
b=Path(sys.argv[1]).read_bytes(); out=Path(sys.argv[2]); out.mkdir(); start=off=count=0
while off < len(b):
 if b[off:off+4] != b'\xa3\x1e\x1c\x00': raise SystemExit(f'bad sync at {off}')
 n=struct.unpack_from('<H',b,off+4)[0]; end=off+6+n+4
 body=b[off+6:off+6+n]
 if len(body)<5 or end>len(b): raise SystemExit('truncated frame')
 off=end
 if body[0:3] == bytes([0x11,5,8]):
  Path(out/f'{count:02d}.f11').write_bytes(b[start:off]); count+=1; start=off
if start != len(b) or count != 6: raise SystemExit(f'jobs={count} trailing={len(b)-start}')
print(count)
PY
for f in "$TMP"/jobs/*.f11; do "$TMP/f11d-real" validate "$f" >/dev/null; done
# Two copies must be collated as complete documents, not page-by-page duplicates.
python3 - "$TMP/jobs" <<'PY'
from pathlib import Path
import hashlib,sys
jobs=sorted(Path(sys.argv[1]).glob('*.f11'))
h=[hashlib.sha256(p.read_bytes()).hexdigest() for p in jobs]
assert len(h)==6 and h[:3]==h[3:] and len(set(h[:3]))==3, h
PY
[[ -z $(find "$TMP/spool" -mindepth 1 -print -quit) ]]
F11D="$TMP/f11d-real" F11_SPOOL="$TMP/spool" F11_OUTPUT_DIR="$TMP/spool" PAGE_HEIGHT="$ROOT/scripts/pdf-page-height.py" MEDIA_CANVAS="$ROOT/scripts/media-canvas.py" \
  "$ROOT/cups/pdftof11" 100 tester short 1 '' "$TMP/short.pdf" >"$TMP/short.f11" 2>"$TMP/short.log"
"$TMP/f11d-real" validate "$TMP/short.f11" | grep -Fq '"rows":256'
[[ -z $(find "$TMP/spool" -mindepth 1 -print -quit) ]]
# Landscape input rotates only media whose long edge still fits the 1,664-dot head.
for spec in '4x6.Fullbleed:812' '5x7.Fullbleed:1015' 'A5.Fullbleed:1678' '8x10.Fullbleed:2030' 'Letter.Fullbleed:2233'; do
  media=${spec%%:*}; rows=${spec##*:}
  F11D="$TMP/f11d-real" F11_SPOOL="$TMP/spool" F11_OUTPUT_DIR="$TMP/spool" PAGE_HEIGHT="$ROOT/scripts/pdf-page-height.py" MEDIA_CANVAS="$ROOT/scripts/media-canvas.py" \
    "$ROOT/cups/pdftof11" 101 tester media 1 "PageSize=$media" "$TMP/short.pdf" >"$TMP/media-$rows.f11" 2>"$TMP/media-$rows.log"
  "$TMP/f11d-real" validate "$TMP/media-$rows.f11" | grep -Fq "\"rows\":$rows"
done
F11D="$TMP/f11d-real" F11_SPOOL="$TMP/spool" F11_OUTPUT_DIR="$TMP/spool" PAGE_HEIGHT="$ROOT/scripts/pdf-page-height.py" MEDIA_CANVAS="$ROOT/scripts/media-canvas.py" \
  "$ROOT/cups/pdftof11" 101 tester portrait 1 'PageSize=4x6.Fullbleed' "$TMP/portrait.pdf" >"$TMP/portrait.f11" 2>"$TMP/portrait.log"
"$TMP/f11d-real" validate "$TMP/portrait.f11" | grep -Fq '"rows":1218'
set +e
F11D="$TMP/f11d-real" F11_SPOOL="$TMP/spool" F11_OUTPUT_DIR="$TMP/spool" PAGE_HEIGHT="$ROOT/scripts/pdf-page-height.py" MEDIA_CANVAS="$ROOT/scripts/media-canvas.py" \
  "$ROOT/cups/pdftof11" 102 tester bad-media 1 'PageSize=A4' "$TMP/short.pdf" >"$TMP/bad-media-output" 2>"$TMP/bad-media.log"
bad_media_rc=$?
set -e
[[ $bad_media_rc -ne 0 && ! -s "$TMP/bad-media-output" ]]
# Media helper output is untrusted: plausible output plus failure, trailing fields,
# oversized dimensions, or nonnumeric dimensions must all fail before stdout.
for case in nonzero extra multiline doublespace leading trailing wide text; do
  helper="$TMP/media-$case"
  case $case in
    nonzero) printf '#!/bin/sh\nprintf "812 1218\\n"\nexit 42\n' >"$helper" ;;
    extra) printf '#!/bin/sh\nprintf "812 1218 extra\\n"\n' >"$helper" ;;
    multiline) printf '#!/bin/sh\nprintf "812 1218\\nEXTRA\\n"\n' >"$helper" ;;
    doublespace) printf '#!/bin/sh\nprintf "812  1218\\n"\n' >"$helper" ;;
    leading) printf '#!/bin/sh\nprintf " 812 1218\\n"\n' >"$helper" ;;
    trailing) printf '#!/bin/sh\nprintf "812 1218 \\n"\n' >"$helper" ;;
    wide) printf '#!/bin/sh\nprintf "1665 1218\\n"\n' >"$helper" ;;
    text) printf '#!/bin/sh\nprintf "812 nope\\n"\n' >"$helper" ;;
  esac
  chmod 0755 "$helper"
  set +e
  F11D="$TMP/f11d-real" F11_SPOOL="$TMP/spool" F11_OUTPUT_DIR="$TMP/spool" PAGE_HEIGHT="$ROOT/scripts/pdf-page-height.py" MEDIA_CANVAS="$helper" \
    "$ROOT/cups/pdftof11" 103 tester "bad-$case" 1 'PageSize=4x6.Fullbleed' "$TMP/short.pdf" >"$TMP/bad-$case-output" 2>"$TMP/bad-$case.log"
  helper_rc=$?
  set -e
  [[ $helper_rc -ne 0 && ! -s "$TMP/bad-$case-output" ]]
done
[[ -z $(find "$TMP/spool" -mindepth 1 -print -quit) ]]
# GNU timeout must force-kill a renderer that ignores SIGTERM.
set +e
timeout --kill-after=1s 1s sh -c 'trap "" TERM; while :; do sleep 1; done' >/dev/null 2>&1
kill_rc=$?
set -e
[[ $kill_rc -eq 137 ]]
# A filter must not emit any bytes until every page has encoded and validated.
cat >"$TMP/f11d-fail" <<EOF
#!/bin/bash
if [[ \$1 == encode-pgm && \$2 == *page-3.pgm ]]; then exit 42; fi
exec "$TMP/f11d-real" "\$@"
EOF
chmod +x "$TMP/f11d-fail"
set +e
F11D="$TMP/f11d-fail" F11_SPOOL="$TMP/spool" F11_OUTPUT_DIR="$TMP/spool" PAGE_HEIGHT="$ROOT/scripts/pdf-page-height.py" MEDIA_CANVAS="$ROOT/scripts/media-canvas.py" \
  "$ROOT/cups/pdftof11" 100 tester fail 1 '' "$TMP/clean.pdf" >"$TMP/failure-output" 2>"$TMP/failure.log"
rc=$?
set -e
[[ $rc -ne 0 && ! -s "$TMP/failure-output" ]]
echo 'filter integration: PASS'
