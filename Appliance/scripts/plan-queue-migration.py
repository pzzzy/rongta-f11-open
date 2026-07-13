#!/usr/bin/env python3
import re
import shlex
import sys
from urllib.parse import urlsplit

current = sys.argv[1] if len(sys.argv) > 1 else ""
managed = re.fullmatch(
    r"device for Rongta_F11: (?:f11:/|usb:(?://)?[^\s]*/F11(?:\?[^\s]+)?)",
    current,
)
if current and not managed:
    raise SystemExit("refusing unmanaged Rongta_F11 queue")

uris = []
for raw in sys.stdin:
    try:
        fields = shlex.split(raw)
    except ValueError:
        continue
    if len(fields) < 5 or fields[0] != "direct" or not fields[1].startswith("usb:"):
        continue
    device_id = fields[4]
    attrs = {}
    for item in device_id.split(";"):
        if ":" in item:
            key, value = item.split(":", 1)
            attrs[key.strip().upper()] = value.strip()
    if attrs.get("MODEL", attrs.get("MDL", "")).upper() != "F11":
        continue
    if urlsplit(fields[1]).path.rstrip("/").split("/")[-1].upper() != "F11":
        continue
    uris.append(fields[1])

unique = sorted(set(uris))
if len(unique) != 1:
    raise SystemExit(f"expected exactly one F11 USB URI, found {len(unique)}")
print(unique[0])
