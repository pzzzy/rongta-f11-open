#!/bin/bash
set -euo pipefail
export LC_ALL=C
PATH=/usr/local/lib/f11:/usr/sbin:/usr/bin:/bin
/usr/local/lib/f11/f11d self-test >/dev/null
[[ -x /usr/lib/cups/filter/pdftof11 ]]
[[ -x /usr/local/lib/f11/plan-queue-migration ]]
[[ -x /usr/local/lib/f11/check-f11-runtime ]]
[[ -r /usr/share/ppd/f11.ppd ]]
[[ ! -e /usr/lib/cups/backend/f11 ]]
grep -Fq '*cupsFilter: "application/pdf 0 pdftof11"' /usr/share/ppd/f11.ppd
cupstestppd -W all /usr/share/ppd/f11.ppd >/dev/null
QUEUE=$(lpstat -v Rongta_F11)
URI=$(/usr/local/lib/f11/check-f11-runtime "$QUEUE" /sys/bus/usb/devices)
lpstat -p Rongta_F11 | grep -Fq ' is idle.  enabled '
lpstat -a Rongta_F11 | grep -Fq 'Rongta_F11 accepting requests'
printf '{"ok":true,"command":"health","detail":{"transport":"cups-usb","uri":"%s"}}\n' "$URI"
