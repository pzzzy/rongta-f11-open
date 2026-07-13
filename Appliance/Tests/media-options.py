#!/usr/bin/env python3
from pathlib import Path
ppd=(Path(__file__).resolve().parents[1]/"cups/f11.ppd").read_text()
expected={"Letter.Fullbleed":"612 792","F11Short":"590 90.8"}
for name,points in expected.items():
 assert f'*PageSize {name}/' in ppd, name
 assert f'<</PageSize[{points}]>>setpagedevice' in ppd, name
 assert f'*PageRegion {name}/' in ppd, name
 assert f'*ImageableArea {name}: "0 0 {points}"' in ppd, name
 assert f'*PaperDimension {name}: "{points}"' in ppd, name
for unsupported in ("4x6", "5x7", "A5", "A4", "Legal"):
 assert f'*PageSize {unsupported}' not in ppd, unsupported
assert '*DefaultPageSize: F11Short' in ppd
assert '*DefaultPageRegion: F11Short' in ppd
assert '*DefaultImageableArea: F11Short' in ppd
assert '*DefaultPaperDimension: F11Short' in ppd
print('media options: PASS')
