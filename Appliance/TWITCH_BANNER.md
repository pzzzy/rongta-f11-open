# Twitch Cheer Banner, Raid Receipt, and Gift-Sub Celebration Printer

The optional `twitch-banner` service turns qualifying Twitch events into large F11 thermal banners. It uses Twitch EventSub WebSockets, so it does not require a public webhook, inbound firewall rule, or port forwarding.

Production behavior:

- A single `channel.cheer` event of at least 1,000 Bits prints one banner.
- Smaller cheers are not accumulated.
- The cheer message is maximized automatically across one to three lines.
- An empty qualifying cheer prints `THANK YOU <user>` (or `THANK YOU ANONYMOUS`).
- A community gift of 10 or more subscriptions prints one native 8.5 × 11-inch celebration page.
- The gift page prominently shows the gift count and gifter, then lists up to 24 correlated recipients.
- Recipient names are correlated from Twitch's `community_sub_gift.id` to each `sub_gift.community_gift_id`.

Broadcaster-only end-to-end testing:

```text
!testbanner YOUR MESSAGE HERE
```

The test command is a real `channel.chat.message` event and follows the same sanitization, durable deduplication, FIFO processing, layout, CUPS submission, and no-retry policy as a paid cheer. It requires an exact immutable Twitch user-ID match to `TWITCH_BROADCASTER_ID`; matching a login/display name, moderator status, or channel role is not sufficient.

The owner can also exercise the complete letter-page path without buying subscriptions:

```text
!testgift Gifter Name | Giftee One, Giftee Two, Giftee Three, Giftee Four, Giftee Five, Giftee Six, Giftee Seven, Giftee Eight, Giftee Nine, Giftee Ten
```

The command requires a gifter, a pipe separator, and 10–100 distinct comma-separated test recipients. It is accepted only from the account whose immutable ID equals `TWITCH_BROADCASTER_ID`.

## Requirements

- The Linux/Raspberry Pi appliance installed and passing its F11 runtime check.
- `/usr/local/bin/bannerprint`, `/usr/local/bin/giftprint`, `/usr/local/bin/raidprint`, and a serial-pinned F11 CUPS queue.
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
- builds native static `twitch-banner` and `giftprint` binaries (including `GOARM=6` on ARMv6);
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

A healthy startup includes all four subscription types:

```text
events=channel.cheer,channel.chat.message,channel.chat.notification,channel.raid
```

## Message handling

- Maximum accepted banner input: 256 UTF-8 bytes and 16 words.
- Control characters and unsupported glyphs are normalized safely.
- The output may differ from the chat rendering; verify the logged sanitized `text` value.
- The command token is case-insensitive, but must be the first complete token and must have non-empty trailing text.
- Only the account whose immutable ID equals `TWITCH_BROADCASTER_ID` can invoke `!testbanner`.
- Ordinary chat messages are ignored.

## Raid celebration receipts

- The service subscribes to Twitch EventSub `channel.raid` using the pinned target condition `to_broadcaster_user_id=TWITCH_BROADCASTER_ID`.
- Incoming raids are accepted only when the target is the configured broadcaster and the source broadcaster is different from the target.
- The receipt puts `RAID INCOMING`, the raiding channel name, and the formatted viewer count at the center of a native 1,664 × 2,233-dot letter-length page.
- The raiding channel name is limited to printable ASCII supported by the embedded font; unsupported display names fall back to Twitch's login when available.
- Each notification is deduplicated as `raid:<EventSub message_id>` before the single physical `raidprint` submission.
- Raid output uses a dedicated structured renderer and never passes raid text through the short-banner layout.

## Gift-sub celebration handling

- `channel.chat.notification` supplies the aggregate `community_sub_gift` notice and each correlated `sub_gift` recipient.
- Only aggregate gifts with `total >= 10` qualify; separate smaller gifts are never accumulated.
- Recipient identity is deduplicated by immutable Twitch recipient user ID, not display name.
- The service collects correlated names for up to 12 seconds. It prints immediately if all names arrive.
- Pending collection survives ordinary EventSub disconnect/reconnect attempts while the daemon remains running, and due gifts continue to flush during reconnect backoff.
- Pending, not-yet-reserved collection is intentionally in memory and does not survive a daemon/process restart. Twitch redelivery may reconstruct it; otherwise that celebration can be missed rather than risk duplicate paper.
- If Twitch omits or delays names, the page honestly lists the names received and adds `+ N MORE`; it never invents recipients.
- Anonymous gifts display `ANONYMOUS` as the gifter.
- The native page is 1,664 × 2,233 dots: the F11's full 8.20-inch printable head width by exactly 11 inches at 203 dpi, centered on letter-width media, with a maximum of 24 displayed names.
- `giftprint` validates the encoded stream by decoding it and comparing every raster row before CUPS submission.
- Unsupported name characters are normalized. Long gifter and recipient names are automatically fitted within bounded regions.

## Exactly-once boundary

Twitch may redeliver events. The service handles them conservatively:

1. Qualifying event IDs are durably appended as `reserved` before printing.
2. Reserved IDs are considered consumed across restarts.
3. One serial event loop invokes `bannerprint --lines auto`, structured `giftprint`, or structured `raidprint` once.
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
- The daemon accesses printing only through the three validated local print executables and the configured CUPS queue.

Treat anyone able to modify `/etc/twitch-banner/environment`, `/usr/local/bin/twitch-banner`, `/usr/local/bin/bannerprint`, `/usr/local/bin/giftprint`, `/usr/local/bin/raidprint`, the systemd unit, or the private token/journal as a trusted appliance administrator.

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
/usr/local/bin/giftprint
/usr/local/bin/raidprint
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

For full removal, disable the service, then remove the unit and all three binaries (`twitch-banner`, `giftprint`, and `raidprint`), helper, protected environment, and private state only after making any required audit backup. OAuth tokens should also be revoked through Twitch account connections or the Twitch token-revocation endpoint.

## Development and verification

From `Appliance/`:

```sh
go test -count=1 ./...
go test -race -count=1 ./internal/twitchbanner ./internal/twitchgift ./internal/giftpage ./internal/twitchraid ./internal/raidpage ./cmd/giftprint ./cmd/raidprint ./cmd/twitch-banner
go vet ./...
bash -n scripts/install-twitch-banner.sh scripts/twitch-banner-authorize
python3 Tests/twitch-banner-architecture.py
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build ./cmd/twitch-banner
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build ./cmd/giftprint
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build ./cmd/raidprint
```

Tests include cheer and gift thresholds, gift correlation, recipient-ID deduplication, out-of-order and timed-out recipient collection, owner-only command parsing, letter-page geometry/coverage/overflow, stream round-trip equality, OAuth state and identity enforcement, immutable broadcaster pinning, subscription request shape, real Twitch payload routing, durable reservation, restart deduplication, corrupt-journal fail-closed behavior, and one-invocation physical submission.

For responsible disclosure, see the repository's [`SECURITY.md`](../SECURITY.md). Contributions follow [`CONTRIBUTING.md`](../CONTRIBUTING.md) and the MIT license.
