# Rongta F11 Open Driver

Clean-room, blob-free Rongta F11 printing for macOS and Linux/Raspberry Pi.

This repository lets applications render documents or generate raster pages and send them directly to an F11 without installing or redistributing Rongta's proprietary driver, filter, PPD, DLLs, or Intel binaries. The Linux appliance includes original clean-room CUPS metadata and a native Go protocol implementation.

> Community interoperability project. Not affiliated with or endorsed by Rongta Technology. “Rongta” and “F11” may be trademarks of their respective owners.

## Features

- Native Apple Silicon Swift library
- PDFKit/CoreGraphics reference renderer
- Deterministic 1-bit ordered-Bayer conversion
- Clean-room Huffman tree construction and serialization
- RTProtocol framing and seeded CRC-32
- Independent generated-stream decoder and validation
- Source-available libusb sender
- Dual CLI/AppKit drag-and-drop reference application
- No proprietary binary blobs
- No Rosetta, CUPS filter, PPD, or Python runtime

## Pipeline

```text
PDF or 8-bit grayscale page
→ 1664 × page-height grayscale canvas
→ 1592-dot / 199-byte monochrome rows
→ Huffman tree + compressed rows
→ A3 1E 1C RTProtocol frames
→ USB bulk OUT endpoint 0x01
→ F11
```

Before USB transmission, the reference app independently decodes the complete generated stream and refuses to print unless every recovered row equals the intended raster.

## Requirements

- macOS 13 or newer
- Swift 6 / Xcode or Command Line Tools
- libusb for physical USB transmission

```bash
brew install libusb
```

Protocol encoding, decoding, and dry runs do not require libusb.

## Native iPhone/iPad printing

The optional AirPrint bridge exposes a USB-connected F11 through the standard iOS Print sheet—no iOS app required. It accepts PDF, JPEG, PNG, and text jobs and preserves multipage documents.

```bash
brew install libusb openssl@3 pkg-config
./Scripts/install-airprint-bridge.sh
```

See [Docs/AIRPRINT.md](Docs/AIRPRINT.md) for the macOS bridge architecture, supported formats, verification, security, logs, and uninstall steps. See [THIRD_PARTY.md](THIRD_PARTY.md) for pinned dependency provenance and licenses.

### Raspberry Pi appliance

The Linux-native Go appliance turns a Raspberry Pi and USB-connected F11 into a persistent CUPS/AirPrint printer. It supports full native 1,664-dot output, continuous landscape banners, images, and repairable multi-page PDFs.

```bash
git clone https://github.com/pzzzy/rongta-f11-open.git
cd rongta-f11-open/Appliance
sudo ./scripts/install.sh
```

See [Appliance/README.md](Appliance/README.md) for installation, firewall setup, security limits, iPhone use, diagnostics, vehicle considerations, and integration tests.

### Twitch cheer banners

The appliance can optionally print a single qualifying Twitch cheer as a maximized one-to-three-line banner. A broadcaster-only `!testbanner` chat command exercises the same real EventSub, sanitization, deduplication, layout, CUPS, and printer path without spending Bits. The integration uses outbound EventSub WebSockets, pins authorization to an immutable Twitch user ID, runs as an unprivileged hardened systemd service, and never retries an ambiguous physical print.

See [Appliance/TWITCH_BANNER.md](Appliance/TWITCH_BANNER.md) for installation, least-privilege OAuth, threat model, exactly-once boundary, operations, rollback, removal, and verification.

## Build and test

```bash
swift run F11CoreTests
swift build -c release
```

Build the self-contained local app bundle:

```bash
./Scripts/build-app.sh
```

Output:

```text
dist/F11 PDF Printer.app
dist/f11print
```

## CLI

```bash
dist/f11print document.pdf
```

Safe dry run:

```bash
dist/f11print --dry-run --output /tmp/f11-preview document.pdf
```

Options:

```text
--dry-run          Generate and validate without USB transmission
--output DIR       Preserve grayscale, PNG preview, and .f11 stream
--density 1-15     Default: 8
--speed N          Default: 16
--copies 1-255     Default: 1
--help
```

## Use the library

Add this repository as a Swift package and depend on `F11PrintCore`.

```swift
import F11PrintCore

let gray = [UInt8](repeating: 255, count: 1664 * 2233)
let stream = try F11JobEncoder.encode(
    gray: gray,
    sourceWidth: 1664,
    sourceHeight: 2233,
    speed: 16,
    density: 8,
    tracking: 0
)

// Strongly recommended before transport:
let decoded = try F11JobDecoder.decode(stream)
precondition(decoded.rows.count == 2233)
```

The library deliberately does not open USB devices. Applications can use the included `Helpers/f11usb.c`, libusb directly, or another transport appropriate to their platform.

## Protocol documentation

- [Protocol specification](Docs/PROTOCOL.md)
- [Architecture and integration](Docs/INTEGRATION.md)
- [Clean-room provenance](Docs/PROVENANCE.md)

The proven device identity is USB VID `0x0FE6`, PID `0x811E`, interface 0, bulk OUT endpoint `0x01`.

## Known scope

The implementation targets the tested Rongta F11 at 203 dpi. It does not claim compatibility with sibling Rongta models, despite shared protocol framing in some vendor packages. Status reads are not required for printing and the tested unit did not return application-level acknowledgements.

The default renderer uses a 24-dot left calibration correction learned from the tested unit. Mechanical alignment may vary; applications can configure rendering offsets.

## Safety

- Start with `--dry-run` and inspect generated artifacts.
- Keep the printer loaded with the correct media.
- Do not send arbitrary or unvalidated `.f11` data.
- The reference app validates framing, CRCs, tree reconstruction, and raster equality before transmitting.

Security issues should be reported according to [SECURITY.md](SECURITY.md).

## License

Original source and documentation in this repository are licensed under the MIT License. See [LICENSE](LICENSE).

libusb is a separate project licensed under LGPL-2.1-or-later. The build script may copy your locally installed libusb dynamic library into the app bundle; that library remains under libusb's license and is not part of this repository's source license.

No Rongta proprietary binary, PPD, disassembly, or extracted source is included.
