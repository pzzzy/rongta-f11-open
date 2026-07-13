#!/usr/bin/env python3
from pathlib import Path

root = Path(__file__).resolve().parents[1]
filter_path = root / "cups/pdftof11"
ppd = (root / "cups/f11.ppd").read_text()
installer = (root / "scripts/install.sh").read_text()
quirk = root / "cups/0fe6-811e.usb-quirks"

assert filter_path.exists(), "missing stdout-only CUPS filter"
assert (root / "scripts/pdf-page-height.py").exists(), "missing page geometry helper"
assert (root / "scripts/media-canvas.py").exists(), "missing media canvas helper"
text = filter_path.read_text()
assert 'PAGE_HEIGHT=${PAGE_HEIGHT:-/usr/local/lib/f11/pdf-page-height}' in text
assert 'pdf-page-height.py" /usr/local/lib/f11/pdf-page-height' in installer
assert 'MEDIA_CANVAS=${MEDIA_CANVAS:-/usr/local/lib/f11/media-canvas}' in text
assert 'media-canvas.py" /usr/local/lib/f11/media-canvas' in installer
assert "f11d send" not in text and '"$F11D" send' not in text
assert "F11_OUTPUT_DIR" in text, "filter needs testable pre-emission staging"
assert "validate" in text
assert 'timeout --kill-after=10s 120s gs -q -dSAFER -dBATCH -dNOPAUSE' in text
assert '-dFirstPage="$page" -dLastPage="$page"' in text
assert '-sDEVICE=pgmraw -r203' in text
assert 'pdftoppm' not in text
assert "cat" in text
assert '*cupsFilter: "application/pdf 0 pdftof11"' in ppd
assert '-v f11:/' not in installer and '-v "f11:/"' not in installer
assert "/usr/lib/cups/backend/usb" in installer
assert "F11_USB_URI" in installer
import re
assert not re.search(r"serial=[A-Za-z0-9]{8,}", installer), "installer embeds a device serial"
assert quirk.read_text().strip() == "0x0fe6 0x811e unidir delay-close"
assert "/usr/share/cups/usb/0fe6-811e.usb-quirks" in installer
assert "cups/f11-migration-hold" in installer
assert installer.index('f11-migration-hold" /usr/lib/cups/backend/f11') < installer.index("apt-get install") < installer.index("systemctl enable --now cups")
assert installer.index("cupsdisable Rongta_F11") < installer.index("cancel -a Rongta_F11") < installer.index('lpadmin -p Rongta_F11 -v "$F11_USB_URI"') < installer.index("rm -f /usr/lib/cups/backend/f11") < installer.index("cupsenable Rongta_F11")
health = root / "scripts/f11-health.sh"
assert health.exists(), "missing non-printing CUPS health check"
health_text = health.read_text()
assert "self-test" in health_text
assert "lpinfo -v" not in health_text
assert "/usr/lib/cups/backend/usb" not in health_text
assert "check-f11-runtime" in health_text
assert 'F11_QUEUE=${F11_QUEUE:-Rongta_F11}' in health_text
assert 'lpstat -v "$F11_QUEUE"' in health_text
assert 'check-f11-runtime "$F11_QUEUE" "$QUEUE"' in health_text
assert "send" not in health_text
service = (root / "systemd/f11-health.service").read_text()
assert "ExecStart=/usr/local/lib/f11/f11-health" in service
print("filter architecture: PASS")
