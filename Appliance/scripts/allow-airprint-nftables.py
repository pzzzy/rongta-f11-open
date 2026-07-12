#!/usr/bin/env python3
import pathlib
import sys

if len(sys.argv) != 2:
    raise SystemExit("usage: allow-airprint-nftables.py /etc/nftables.conf")
path = pathlib.Path(sys.argv[1])
text = path.read_text()
old4 = "ip saddr 192.168.0.0/16 tcp dport { 22, 8080 } accept"
new4 = old4.replace("22, 8080", "22, 631, 8080") + "\n  ip saddr 192.168.0.0/16 udp dport 5353 accept"
old6 = "ip6 saddr fe80::/10 tcp dport { 22, 8080 } accept"
new6 = old6.replace("22, 8080", "22, 631, 8080") + "\n  ip6 saddr fe80::/10 udp dport 5353 accept"
if "tcp dport { 22, 631, 8080 }" in text and "udp dport 5353" in text:
    raise SystemExit(0)
if text.count(old4) != 1 or text.count(old6) != 1:
    raise SystemExit("unsupported nftables.conf layout; add LAN TCP 631 and UDP 5353 manually")
path.write_text(text.replace(old4, new4).replace(old6, new6))
