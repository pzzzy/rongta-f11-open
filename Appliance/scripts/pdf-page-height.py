#!/usr/bin/env python3
import re
import subprocess
import sys

dimensions = len(sys.argv) == 4 and sys.argv[1] == "--dimensions"
if dimensions:
    pdf, page_arg = sys.argv[2], sys.argv[3]
elif len(sys.argv) == 3:
    pdf, page_arg = sys.argv[1], sys.argv[2]
else:
    raise SystemExit("usage: pdf-page-height.py [--dimensions] PDF PAGE")
page = int(page_arg)
if page < 1 or page > 20:
    raise SystemExit("page out of range")
r = subprocess.run(
    ["pdfinfo", "-f", str(page), "-l", str(page), pdf],
    text=True,
    capture_output=True,
    timeout=20,
)
if r.returncode:
    raise SystemExit("pdfinfo failed")
m = re.search(rf"^Page\s+{page}\s+size:\s+([0-9]+(?:\.[0-9]+)?)\s+x\s+([0-9]+(?:\.[0-9]+)?)\s+pts\b", r.stdout, re.M)
rotation = re.search(rf"^Page\s+{page}\s+rot:\s+(-?[0-9]+)\s*$", r.stdout, re.M)
if not m or not rotation:
    raise SystemExit("page geometry unavailable")
width_pt, height_pt = map(float, m.groups())
rotation_degrees = int(rotation.group(1)) % 360
if rotation_degrees not in (0, 90, 180, 270):
    raise SystemExit("unsupported page rotation")
if rotation_degrees in (90, 270):
    width_pt, height_pt = height_pt, width_pt
if not (36 <= width_pt <= 1296 and 7.2 <= height_pt <= 792):
    raise SystemExit("page dimensions out of range")
height_px = round(height_pt * 203 / 72)
width_px = round(width_pt * 203 / 72)
if not (20 <= width_px <= 3654 and 20 <= height_px <= 2233):
    raise SystemExit("raster height out of range")
if dimensions:
    print(width_px, height_px)
else:
    print(height_px)
