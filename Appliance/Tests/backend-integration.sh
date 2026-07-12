#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
for cmd in go qpdf pdftoppm; do command -v "$cmd" >/dev/null || { echo "SKIP: missing $cmd"; exit 0; }; done
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cd "$ROOT"
go build -o "$TMP/f11d" ./cmd/f11d
python3 - "$TMP/clean.pdf" <<'PY'
from pathlib import Path
import sys
pages=[]
for n in range(1,6):
    stream=f"BT /F1 36 Tf 72 720 Td (F11 TEST PAGE {n}) Tj ET".encode()
    pages.append(stream)
objs=[b'<< /Type /Catalog /Pages 2 0 R >>']
kids=' '.join(f'{3+i*2} 0 R' for i in range(5)).encode()
objs.append(b'<< /Type /Pages /Kids ['+kids+b'] /Count 5 >>')
font_id=13
for i,s in enumerate(pages):
    content=4+i*2
    objs.append(f'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 {font_id} 0 R >> >> /Contents {content} 0 R >>'.encode())
    objs.append(f'<< /Length {len(s)} >>\nstream\n'.encode()+s+b'\nendstream')
objs.append(b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>')
out=bytearray(b'%PDF-1.4\n'); offsets=[0]
for i,obj in enumerate(objs,1):
    offsets.append(len(out)); out+=f'{i} 0 obj\n'.encode()+obj+b'\nendobj\n'
xref=len(out); out+=f'xref\n0 {len(objs)+1}\n0000000000 65535 f \n'.encode()
for off in offsets[1:]: out+=f'{off:010d} 00000 n \n'.encode()
out+=f'trailer << /Size {len(objs)+1} /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n'.encode()
Path(sys.argv[1]).write_bytes(out)
PY
qpdf --check "$TMP/clean.pdf" >/dev/null
cp "$TMP/clean.pdf" "$TMP/repairable.pdf"
python3 - "$TMP/repairable.pdf" <<'PY'
from pathlib import Path
import re,sys
p=Path(sys.argv[1]); b=p.read_bytes()
# Point object 6's xref entry at zero. qpdf status 3 must be repairable.
start=int(re.search(br'startxref\n(\d+)',b).group(1)); lines=b[start:].splitlines()
lines[8]=b'0000000000 00000 n '
p.write_bytes(b[:start]+b'\n'.join(lines)+b'\n')
PY
set +e
qpdf --check "$TMP/repairable.pdf" >/dev/null 2>&1
rc=$?
set -e
[[ $rc -eq 3 ]]
for pdf in clean repairable; do
    mkdir "$TMP/spool-$pdf"
    copies=1
    expected=5
    [[ $pdf == repairable ]] && { copies=2; expected=10; }
    F11D="$TMP/f11d" F11_SPOOL="$TMP/spool-$pdf" F11_LOCK="$TMP/$pdf.lock" F11_DRY_RUN=1 \
      "$ROOT/cups/f11" 99 tester "$pdf" "$copies" '' "$TMP/$pdf.pdf" >"$TMP/$pdf.log"
    [[ $(grep -c '"command":"validate"' "$TMP/$pdf.log") -eq $expected ]]
    [[ -z $(find "$TMP/spool-$pdf" -mindepth 1 -print -quit) ]]
done
echo 'backend integration: PASS'
