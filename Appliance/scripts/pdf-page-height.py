#!/usr/bin/env python3
import re
import subprocess
import sys

if len(sys.argv) != 3:
    raise SystemExit("usage: pdf-page-height.py PDF PAGE")
page = int(sys.argv[2])
if page < 1 or page > 20:
    raise SystemExit("page out of range")
r = subprocess.run(
    ["pdfinfo", "-f", str(page), "-l", str(page), sys.argv[1]],
    text=True,
    capture_output=True,
    timeout=20,
)
if r.returncode:
    raise SystemExit("pdfinfo failed")
m = re.search(rf"^Page\s+{page}\s+size:\s+([0-9]+(?:\.[0-9]+)?)\s+x\s+([0-9]+(?:\.[0-9]+)?)\s+pts\b", r.stdout, re.M)
if not m:
    raise SystemExit("page size unavailable")
width_pt, height_pt = map(float, m.groups())
if not (36 <= width_pt <= 1296 and 7.2 <= height_pt <= 792):
    raise SystemExit("page dimensions out of range")
height_px = round(height_pt * 203 / 72)
if not (20 <= height_px <= 2233):
    raise SystemExit("raster height out of range")
print(height_px)
