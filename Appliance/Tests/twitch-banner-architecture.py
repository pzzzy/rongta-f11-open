#!/usr/bin/env python3
from pathlib import Path
root=Path(__file__).resolve().parents[1]
unit=(root/'systemd/twitch-banner.service').read_text()
installer=(root/'scripts/install-twitch-banner.sh').read_text()
assert 'User=twitch-banner' in unit
assert 'NoNewPrivileges=true' in unit
assert 'PrivateDevices=true' in unit
assert 'ProtectSystem=strict' in unit
assert 'TWITCH_CLIENT_SECRET=%s' in installer
assert "read -r -s" in installer
assert 'env $(cat' not in installer
assert 'GOARM=6' in installer
assert 'command -v bannerprint' in installer
assert 'check-f11-runtime' in installer
assert 'twitch-banner-authorize' in installer
assert 'twitch-banner run' in unit
assert 'go build' in installer and './cmd/twitch-banner' in installer
assert './cmd/giftprint' in installer
assert './cmd/raidprint' in installer
assert 'install -o root -g root -m0755 "$BUILD/giftprint" /usr/local/bin/giftprint' in installer
assert 'install -o root -g root -m0755 "$BUILD/raidprint" /usr/local/bin/raidprint' in installer
assert 'raidprint' in installer
assert 'F11_QUEUE=' in installer
assert 'TWITCH_BROADCASTER_ID=' in installer
print('twitch banner architecture: PASS')
