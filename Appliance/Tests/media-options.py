#!/usr/bin/env python3
from pathlib import Path
import subprocess

root=Path(__file__).resolve().parents[1]
ppd=(root/"cups/f11.ppd").read_text()
helper=root/"scripts/media-canvas.py"
expected={
 "F11Short":("590 90.8",(1664,256),"custom_208.14x32.03mm_208.14x32.03mm"),
 "4x6.Fullbleed":("288 432",(812,1218),"na_index-4x6_4x6in"),
 "5x7.Fullbleed":("360 504",(1015,1421),"na_5x7_5x7in"),
 "A5.Fullbleed":("419.528 595.276",(1183,1678),"iso_a5_148x210mm"),
 "8x10.Fullbleed":("576 720",(1624,2030),"na_govt-letter_8x10in"),
 "Letter.Fullbleed":("612 792",(1664,2233),"na_letter_8.5x11in"),
}
for name,(points,canvas,pwg) in expected.items():
 assert f'*PageSize {name}/' in ppd, name
 assert f'<</PageSize[{points}]>>setpagedevice' in ppd, name
 assert f'*PageRegion {name}/' in ppd, name
 assert f'*ImageableArea {name}: "0 0 {points}"' in ppd, name
 assert f'*PaperDimension {name}: "{points}"' in ppd, name
 out=subprocess.check_output([helper,f'PageSize={name}'],text=True).strip()
 assert out==f'{canvas[0]} {canvas[1]}',(name,out)
 assert subprocess.check_output([helper,f'media={pwg}'],text=True).strip()==out
 assert subprocess.check_output([helper,f'PageSize={name} media={pwg}'],text=True).strip()==out
assert subprocess.check_output([helper,''],text=True).strip()=='1664 256'
for bad in ('PageSize=A4','media=Legal','PageSize=F11Short media=na_index-4x6_4x6in','PageSize="unterminated'):
 assert subprocess.run([helper,bad],stdout=subprocess.PIPE,stderr=subprocess.PIPE).returncode!=0,bad
assert '*DefaultPageSize: F11Short' in ppd
assert '*DefaultPageRegion: F11Short' in ppd
assert '*DefaultImageableArea: F11Short' in ppd
assert '*DefaultPaperDimension: F11Short' in ppd
print('media options: PASS')
