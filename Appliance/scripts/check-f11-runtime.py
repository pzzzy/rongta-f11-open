#!/usr/bin/env python3
import pathlib
import sys
from urllib.parse import parse_qs, urlsplit

if len(sys.argv) != 3:
    raise SystemExit("usage: check-f11-runtime.py QUEUE_LINE SYSFS_ROOT")
prefix = "device for Rongta_F11: "
if not sys.argv[1].startswith(prefix):
    raise SystemExit("unexpected queue identity")
uri = sys.argv[1][len(prefix):]
parsed = urlsplit(uri)
if parsed.scheme != "usb" or parsed.path.rstrip("/").split("/")[-1].upper() != "F11":
    raise SystemExit("queue is not an F11 USB URI")
serials = parse_qs(parsed.query, strict_parsing=False).get("serial", [])
if len(serials) > 1 or (serials and not serials[0]):
    raise SystemExit("invalid queue serial")
expected_serial = serials[0] if serials else None
matches = []
for device in pathlib.Path(sys.argv[2]).glob("*"):
    try:
        vid = (device / "idVendor").read_text().strip().lower()
        pid = (device / "idProduct").read_text().strip().lower()
    except (OSError, UnicodeError):
        continue
    if (vid, pid) != ("0fe6", "811e"):
        continue
    try:
        serial = (device / "serial").read_text().strip()
    except (OSError, UnicodeError):
        serial = ""
    if expected_serial is None or serial == expected_serial:
        matches.append((str(device), serial))
if len(matches) != 1:
    raise SystemExit(f"expected exactly one configured F11 USB device, found {len(matches)}")
print(uri)
