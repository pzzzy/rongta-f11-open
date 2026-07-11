# Native iOS printing with the F11 AirPrint bridge

The bridge makes a USB-connected Rongta F11 appear in the native iPhone and iPad Print sheet. No Rongta iOS app, proprietary driver, PPD, or binary blob is used.

## Architecture

```text
iPhone/iPad Print sheet
→ Bonjour (`_ipp._tcp`, `_universal`)
→ IPP Everywhere server
→ PDF/JPEG/PNG/text normalization
→ F11PrintCore clean-room encoder
→ independent stream decode and raster equality check
→ libusb bulk transfer
→ Rongta F11
```

The IPP server is the open-source PWG `ippsample` reference server, pinned to commit `ce11296293da30a111615a2deddaea51268b49ce`. The installer builds it from source under `~/.local/ippsample`. Checked build-time patches correct an upstream resolution-attribute typo, admit only one active job, and restrict generated media capabilities to Letter.

## Supported iOS content

Any application that exposes the native iOS Print action can submit a job. The bridge advertises and accepts:

- `application/pdf`
- `image/jpeg`
- `image/png`
- `text/plain`

Incoming non-PDF documents are normalized to PDF by the open-source `ipptransform` tool. Safari pages, Mail messages, Notes, Files documents, Photos, and other printable application content are therefore handled through the format iOS selects.

## Multipage behavior

PDF jobs may contain multiple pages. The normalizer preserves all pages, and `f11print` renders and prints every page in order. One job is admitted at a time; after receipt, documents above 100 MiB are rejected, and PDFKit rejects normalized output above 200 pages. Override for a trusted deployment with:

```bash
F11_AIRPRINT_MAX_PAGES=300
F11_AIRPRINT_MAX_BYTES=157286400
```

## Install and start

Requirements:

- Apple Silicon Mac running macOS 13 or newer
- Xcode Command Line Tools
- Homebrew `libusb` and OpenSSL dependencies used by the builds
- Git
- F11 connected by USB
- Mac and iPhone/iPad on the same trusted LAN

Run:

```bash
brew install libusb openssl@3 pkg-config
./Scripts/install-airprint-bridge.sh
```

The installer:

1. Builds the clean-room Swift application.
2. Fetches the pinned open-source IPP reference implementation.
3. Builds it as arm64 with TLS support.
4. Installs files below `~/.local/rongta-f11` and `~/.local/ippsample`.
5. Creates `~/Library/LaunchAgents/com.pzzzy.f11-airprint.plist`.
6. Starts the bridge on TCP port 8631.

No `sudo` or system-wide printer-driver installation is required.

## Print from iOS

1. Put the iPhone/iPad on the same Wi-Fi network as the Mac.
2. Open content in Safari, Mail, Files, Photos, Notes, or another application.
3. Choose Share → Print.
4. Select **Rongta F11**.
5. Choose copies/pages as available and tap Print.

## Verify the service

```bash
launchctl print gui/$(id -u)/com.pzzzy.f11-airprint
ippfind -T 5 _ipp._tcp,_universal --name 'Rongta F11' --print
ipptool -tv ipp://localhost:8631/ipp/print Tests/ipp/get-printer.test
```

Expected discovery URI:

```text
ipp://<mac-hostname>.local:8631/ipp/print
```

## Logs and state

```text
~/Library/Application Support/F11AirPrint/
├── logs/stdout.log
├── logs/stderr.log
├── spool/
└── state/
```

Job input and temporary normalized files are deleted after processing. Generated streams/previews are temporary in normal mode. Set `F11_AIRPRINT_DRY_RUN=1` only for diagnostics; that mode retains generated artifacts under the state directory and does not transmit USB data.

## Stop or restart

```bash
launchctl kickstart -k gui/$(id -u)/com.pzzzy.f11-airprint

launchctl bootout gui/$(id -u)/com.pzzzy.f11-airprint
```

## Uninstall

```bash
launchctl bootout gui/$(id -u)/com.pzzzy.f11-airprint 2>/dev/null || true
rm -f ~/Library/LaunchAgents/com.pzzzy.f11-airprint.plist
rm -rf ~/.local/rongta-f11
rm -rf ~/.local/ippsample
rm -rf ~/Library/Application\ Support/F11AirPrint
```

## Security model

- Designed for a trusted home/LAN network, not direct Internet exposure.
- IPP server listens on LAN interfaces for AirPrint discovery.
- Unsupported formats are rejected.
- Only one network job is admitted at a time. After that job is received, it is rejected if larger than 100 MiB; normalized output is rejected above 200 pages. This bounds concurrent spool growth but is not a pre-upload byte quota.
- Jobs are serialized through a cross-process printer lock.
- Spool and state directories are user-private.
- Local `file:` URI printing is not enabled.
- Generated F11 streams are independently decoded and compared to the intended raster before USB transmission.
- The low-level USB helper is not exposed over the network.

If the Mac firewall prompts for incoming connections, allow `ippserver` on private networks so iOS devices can reach it.
