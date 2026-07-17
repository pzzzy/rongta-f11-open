# Twitch Cheer and Chat-Test Banner Printer

The optional `twitch-banner` service turns qualifying Twitch events into large F11 thermal banners. It uses Twitch EventSub WebSockets, so it does not require a public webhook, inbound firewall rule, or port forwarding.

Production behavior:

- A single `channel.cheer` event of at least 1,000 Bits prints one banner.
- Smaller cheers are not accumulated.
- The cheer message is maximized automatically across one to three lines.
- An empty qualifying cheer prints `THANK YOU <user>` (or `THANK YOU ANONYMOUS`).

Broadcaster-only end-to-end testing:

```text
!testbanner YOUR MESSAGE HERE
```

The test command is a real `channel.chat.message` event and follows the same sanitization, durable deduplication, FIFO processing, layout, CUPS submission, and no-retry policy as a paid cheer. It requires an exact immutable Twitch user-ID match to `TWITCH_BROADCASTER_ID`; matching a login/display name, moderator status, or channel role is not sufficient.

## Requirements

- The Linux/Raspberry Pi appliance installed and passing its F11 runtime check.
- `/usr/local/bin/bannerprint` and a serial-pinned F11 CUPS queue.
- A Twitch application configured with this exact OAuth redirect URI:

```text
http://localhost:17563/twitch/callback
```

- The channel's Twitch login and immutable numeric user ID.

Do not commit the Twitch Client Secret, OAuth token, environment file, or event journal.

## Install

Run from `Appliance/` on the Pi. The installer prompts for the Client Secret without placing it in process arguments or shell history.

```sh
sudo env \
  TWITCH_CLIENT_ID='your-client-id' \
  TWITCH_CHANNEL='your_channel_login' \
  TWITCH_BROADCASTER_ID='12345678' \
  F11_QUEUE='Rongta_F11_Media' \
  ./scripts/install-twitch-banner.sh
```

The installer:

- verifies `bannerprint`, the configured CUPS queue, and the attached F11;
- runs Go tests and vet;
- builds a native static binary (including `GOARM=6` on ARMv6);
- creates the unprivileged `twitch-banner` service account;
- writes `/etc/twitch-banner/environment` as `root:twitch-banner` mode `0640`;
- creates private state under `/var/lib/twitch-banner/`;
- installs and enables, but does not start, the hardened systemd service.

## Authorize

The token needs only:

```text
bits:read user:read:chat
```

On a trusted workstation, forward the localhost callback to the Pi:

```sh
ssh -N -L 17563:127.0.0.1:17563 pi@rongbridge.local
```

In a second Pi shell:

```sh
sudo /usr/local/sbin/twitch-banner-authorize
```

Open the printed URL on the workstation and authorize the configured broadcaster. The helper:

- sources the root-managed environment without exposing the Client Secret in arguments;
- runs OAuth as the dedicated service account;
- validates Client ID, login, immutable user ID, `bits:read`, and `user:read:chat`;
- atomically installs the token as `twitch-banner:twitch-banner` mode `0600`.

Start and verify:

```sh
sudo systemctl start twitch-banner.service
sudo systemctl status twitch-banner.service
sudo journalctl -u twitch-banner.service -n 50 --no-pager
```

A healthy startup includes both subscription types:

```text
events=channel.cheer,channel.chat.message
```

## Message handling

- Maximum accepted banner input: 256 UTF-8 bytes and 16 words.
- Control characters and unsupported glyphs are normalized safely.
- The output may differ from the chat rendering; verify the logged sanitized `text` value.
- The command token is case-insensitive, but must be the first complete token and must have non-empty trailing text.
- Only the account whose immutable ID equals `TWITCH_BROADCASTER_ID` can invoke `!testbanner`.
- Ordinary chat messages are ignored.

## Exactly-once boundary

Twitch may redeliver events. The service handles them conservatively:

1. Qualifying event IDs are durably appended as `reserved` before printing.
2. Reserved IDs are considered consumed across restarts.
3. One serial worker invokes `bannerprint --lines auto` once.
4. A successful CUPS submission is appended as `submitted` with its job ID.
5. The service never automatically retries after reservation, including ambiguous failures.

This provides at-most-once behavior at the physical side-effect boundary. It deliberately prefers a missed banner over duplicate paper when outcome is uncertain. Because the F11 has no mechanical paper acknowledgement, CUPS completion is not proof of visible output.

The journal is `/var/lib/twitch-banner/events.jsonl`. Any malformed or empty record causes startup to fail closed. Never truncate or hand-edit the journal while the service is running.

## Security model

- No public inbound listener; EventSub uses an outbound WebSocket.
- OAuth callback binds to Pi loopback only and runs only during authorization.
- Broadcaster authorization is pinned to an immutable numeric Twitch ID.
- Secrets are excluded from command arguments and logs.
- The daemon runs as `twitch-banner`, not root.
- systemd enables `NoNewPrivileges`, private devices, strict filesystem protection, restricted address families, syscall filtering, and a private writable state directory.
- The daemon accesses printing only through `bannerprint` and the configured CUPS queue.

Treat anyone able to modify `/etc/twitch-banner/environment`, `/usr/local/bin/twitch-banner`, the systemd unit, or the private token/journal as a trusted appliance administrator.

## Operations

```sh
sudo systemctl status twitch-banner.service
sudo journalctl -u twitch-banner.service
sudo systemctl restart twitch-banner.service
lpstat -W not-completed -o
/usr/local/lib/f11/f11-health
```

Before an upgrade, stop the service and make a root-only backup of:

```text
/usr/local/bin/twitch-banner
/etc/twitch-banner/environment
/var/lib/twitch-banner/token.json
/var/lib/twitch-banner/events.jsonl
```

To roll back, stop the service, restore all files from the same backup set with their original ownership/modes, run `systemctl daemon-reload` if the unit changed, and start the service. Never restore only the journal or only the binary across incompatible versions.

## Disable or remove

Disable without deleting credentials/state:

```sh
sudo systemctl disable --now twitch-banner.service
```

For full removal, disable the service, then remove the unit, binary, helper, protected environment, and private state only after making any required audit backup. OAuth tokens should also be revoked through Twitch account connections or the Twitch token-revocation endpoint.

## Development and verification

From `Appliance/`:

```sh
go test -count=1 ./...
go test -race -count=1 ./internal/twitchbanner ./cmd/twitch-banner
go vet ./...
bash -n scripts/install-twitch-banner.sh scripts/twitch-banner-authorize
python3 Tests/twitch-banner-architecture.py
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build ./cmd/twitch-banner
```

Tests include threshold/fallback handling, sanitization bounds, OAuth state and identity enforcement, immutable broadcaster pinning, subscription request shape, real Twitch chat payload routing, durable reservation, restart deduplication, corrupt-journal fail-closed behavior, and one-invocation banner submission.

For responsible disclosure, see the repository's [`SECURITY.md`](../SECURITY.md). Contributions follow [`CONTRIBUTING.md`](../CONTRIBUTING.md) and the MIT license.
