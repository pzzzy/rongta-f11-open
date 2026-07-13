#!/usr/bin/env python3
import shlex
import sys

MEDIA = {
    "F11Short": (1664, 256),
    "custom_208.14x32.03mm_208.14x32.03mm": (1664, 256),
    "4x6.Fullbleed": (812, 1218),
    "na_index-4x6_4x6in": (812, 1218),
    "5x7.Fullbleed": (1015, 1421),
    "na_5x7_5x7in": (1015, 1421),
    "A5.Fullbleed": (1183, 1678),
    "iso_a5_148x210mm": (1183, 1678),
    "8x10.Fullbleed": (1624, 2030),
    "na_govt-letter_8x10in": (1624, 2030),
    "Letter.Fullbleed": (1664, 2233),
    "na_letter_8.5x11in": (1664, 2233),
}

if len(sys.argv) != 2:
    raise SystemExit("usage: media-canvas.py CUPS_OPTIONS")
selected = []
try:
    tokens = shlex.split(sys.argv[1])
except ValueError:
    raise SystemExit("invalid CUPS options")
for token in tokens:
    if "=" not in token:
        continue
    key, value = token.split("=", 1)
    if key in ("PageSize", "media"):
        selected.append(value)
if not selected:
    selected = ["F11Short"]
if any(value not in MEDIA for value in selected):
    raise SystemExit("unsupported or conflicting media")
geometries = {MEDIA[value] for value in selected}
if len(geometries) != 1:
    raise SystemExit("unsupported or conflicting media")
print(*geometries.pop())
