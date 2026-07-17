# Twitch Cheer Banner Printer

A single `channel.cheer` event of at least 1,000 Bits prints one maximized banner through the validated `bannerprint`/CUPS/F11 path.

## Authorization

The Twitch application redirect URI must be exactly:

```text
http://localhost:17563/twitch/callback
```

On the Mac, forward the callback port without exposing it publicly:

```sh
ssh -N -L 17563:127.0.0.1:17563 pi@rongbridge.local
```

In a second Pi shell run:

```sh
sudo /usr/local/sbin/twitch-banner-authorize
```

Open the printed URL on the Mac and authorize the configured broadcaster. The helper loads the protected environment without placing the client secret in command arguments, runs OAuth as the dedicated service account, and verifies token ownership and mode.

## Safety

- Event IDs are durably reserved before printing. Reserved events are never retried automatically.
- Any malformed journal record prevents startup and requires explicit operator recovery.
- EventSub reconnect messages use subscription-preserving socket handoff; fresh connections create one subscription.
- The OAuth token must match the configured Twitch Client ID and broadcaster and include `bits:read`.
- Qualifying events are handled serially; exactly one `bannerprint --lines auto` command is invoked.
- The printer has no mechanical acknowledgement channel. A successful CUPS submission is not proof of visible output.

## Operations

```sh
sudo systemctl status twitch-banner.service
sudo journalctl -u twitch-banner.service
sudo systemctl restart twitch-banner.service
```

The service state is private under `/var/lib/twitch-banner/`. Do not edit or truncate `events.jsonl`; corruption deliberately fails closed.
