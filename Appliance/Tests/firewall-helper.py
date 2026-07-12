#!/usr/bin/env python3
import pathlib
import subprocess
import sys
import tempfile

helper = pathlib.Path(__file__).resolve().parents[1] / "scripts" / "allow-airprint-nftables.py"
base = """table inet filter {
 chain input {
  ip saddr 192.168.0.0/16 tcp dport { 22, 8080 } accept
  ip6 saddr fe80::/10 tcp dport { 22, 8080 } accept
 }
}
"""
required = (
    "ip saddr 192.168.0.0/16 tcp dport { 22, 631, 8080 } accept",
    "ip saddr 192.168.0.0/16 udp dport 5353 accept",
    "ip6 saddr fe80::/10 tcp dport { 22, 631, 8080 } accept",
    "ip6 saddr fe80::/10 udp dport 5353 accept",
)

def run(source: pathlib.Path, output: pathlib.Path, ok: bool) -> None:
    result = subprocess.run([sys.executable, str(helper), str(source), str(output)], capture_output=True, text=True)
    if (result.returncode == 0) != ok:
        raise AssertionError(f"unexpected status {result.returncode}: {result.stderr}")

with tempfile.TemporaryDirectory() as td:
    root = pathlib.Path(td)
    source, output = root / "source.nft", root / "output.nft"
    source.write_text(base)
    run(source, output, True)
    transformed = output.read_text()
    assert all(rule in transformed for rule in required)

    second = root / "second.nft"
    run(output, second, True)
    assert second.read_bytes() == output.read_bytes()

    partial = root / "partial.nft"
    partial.write_text(transformed.replace(required[-1] + "\n", ""))
    rejected = root / "rejected.nft"
    run(partial, rejected, False)
    assert not rejected.exists()

    unknown = root / "unknown.nft"
    unknown.write_text("table inet filter {}\n")
    run(unknown, rejected, False)
    assert not rejected.exists()

print("firewall helper: PASS")
