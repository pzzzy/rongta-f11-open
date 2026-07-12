#!/usr/bin/env python3
import pathlib
import sys

if len(sys.argv) != 3:
    raise SystemExit("usage: allow-airprint-nftables.py INPUT OUTPUT")
source = pathlib.Path(sys.argv[1])
destination = pathlib.Path(sys.argv[2])
text = source.read_text()
old4 = "ip saddr 192.168.0.0/16 tcp dport { 22, 8080 } accept"
new4 = "ip saddr 192.168.0.0/16 tcp dport { 22, 631, 8080 } accept\n  ip saddr 192.168.0.0/16 udp dport 5353 accept"
old6 = "ip6 saddr fe80::/10 tcp dport { 22, 8080 } accept"
new6 = "ip6 saddr fe80::/10 tcp dport { 22, 631, 8080 } accept\n  ip6 saddr fe80::/10 udp dport 5353 accept"
required = (
    "ip saddr 192.168.0.0/16 tcp dport { 22, 631, 8080 } accept",
    "ip saddr 192.168.0.0/16 udp dport 5353 accept",
    "ip6 saddr fe80::/10 tcp dport { 22, 631, 8080 } accept",
    "ip6 saddr fe80::/10 udp dport 5353 accept",
)
present = tuple(rule in text for rule in required)
if all(present):
    result = text
elif any(present):
    raise SystemExit("partial F11 AirPrint rules found; refusing automatic modification")
elif text.count(old4) == 1 and text.count(old6) == 1:
    result = text.replace(old4, new4).replace(old6, new6)
else:
    raise SystemExit("unsupported nftables.conf layout; add trusted-LAN TCP 631 and UDP 5353 manually")
destination.write_text(result)
