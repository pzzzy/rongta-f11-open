# Raspberry Pi AirPrint appliance

This directory contains the Linux-native Rongta F11 driver and a Raspberry Pi CUPS/AirPrint appliance.

It was physically verified on a Raspberry Pi 4 running Debian 13 (`arm64`) with an F11 at USB `0fe6:811e`. The protocol core also cross-builds for Raspberry Pi OS `armhf`/ARMv7.

## What works

- Native Go implementation of F11 framing, seeded CRC-32, deterministic Huffman coding, raster encoding, strict decoding, and direct Linux usbfs transport.
- Exact Swift/Go protocol fixture identity.
- Proven 1,592-dot and full native 1,664-dot raster modes.
- Full-width continuous landscape banners generated directly on Linux.
- CUPS shared queue discoverable by native iOS AirPrint.
- PDF, JPEG, PNG, PostScript, PWG Raster, and URF inputs normalized by CUPS to PDF.
- Clean and recoverably malformed PDFs normalized with qpdf.
- Multi-page PDFs up to 20 pages, processed and sent one page at a time.
- Independent decode validation before every USB transmission.
- Serialized jobs, bounded input, per-command timeouts, private temporary directories, and automatic cleanup.

## Architecture

```text
iPhone/macOS
  -> Bonjour + IPP/IPPS (CUPS)
  -> CUPS document normalization
  -> custom f11:/ backend
  -> qpdf repair and validation
  -> Poppler 203-dpi grayscale raster
  -> native Go F11 encoder
  -> strict independent decoder
  -> serialized usbfs bulk transport
  -> Rongta F11
```

Documents never directly control raw USB output. Only streams accepted by the strict F11 grammar are transmitted.

## Install

Start with a current 32- or 64-bit Raspberry Pi OS/Debian installation and network connectivity:

```bash
git clone https://github.com/pzzzy/rongta-f11-open.git
cd rongta-f11-open/Appliance
sudo ./scripts/install.sh
```

The installer:

- installs maintained Debian CUPS, Avahi, qpdf, Poppler, Ghostscript, and Go packages;
- runs Go tests and vet before installation;
- builds `f11d` locally for the Pi architecture;
- creates the unprivileged `f11print` service account;
- installs narrow udev permissions for `0fe6:811e`;
- prevents `usblp` from claiming the interface used by direct usbfs;
- installs the CUPS backend and clean-room PPD;
- creates and shares `Rongta_F11`;
- enables a fail-closed boot-time protocol/USB health check (installation still succeeds if the printer is temporarily disconnected).

The installer does not alter an existing firewall by default. Permit trusted-LAN TCP 631 and UDP 5353 manually, or use the narrow known-layout helper:

```bash
sudo ./scripts/install.sh --configure-nftables
```

That option backs up `/etc/nftables.conf`, refuses unknown layouts, validates the resulting ruleset, and then loads it.

## iPhone use

1. Connect the iPhone and Pi to the same trusted LAN.
2. Open an image or PDF and select **Share -> Print**.
3. Choose **Rongta F11 Pi AirPrint @ <hostname>**.
4. Submit one copy initially and confirm physical output.

If an older Mac bridge advertised the same name, stop it or rename one service. Colliding Bonjour names can cause iOS to query the wrong host.

## Diagnostics

```bash
/usr/local/lib/f11/f11d self-test
/usr/local/lib/f11/f11d probe
sudo -u f11print /usr/local/lib/f11/f11d diagnose
lpstat -t
ipptool -t ipp://localhost/printers/Rongta_F11 \
  /usr/share/cups/ipptool/get-printer-attributes.test
```

Expected deterministic self-test:

```text
bytes:  913
rows:   8
sha256: bffe45513da30e7fc29b4e404154cb65a87637a1df1951929fa49f248f4627f4
```

## Native banner generation

Generate a continuous 15 x 8.2 inch landscape banner entirely on the Pi:

```bash
/usr/local/lib/f11/f11d banner /tmp/banner.f11 \
  "PLEASE DON'T PARK YOUR TRAILER HERE"
/usr/local/lib/f11/f11d validate /tmp/banner.f11
/usr/local/lib/f11/f11d send /tmp/banner.f11
```

`banner` chooses a semantic two-line break, fits an embedded open Go Bold font, rotates into feed orientation, and verifies all decoded rows before writing the stream.

## Limits and safety

- Input PDF: 64 MiB maximum after CUPS normalization.
- Pages: 1-20.
- Raster: 1,664 x 2,233, 203 dpi grayscale per ordinary page.
- Copies: the CUPS backend expands the requested count (1-255); each encoded page uses one protocol copy.
- One backend job holds `/run/lock/f11-print.lock` at a time.
- Temporary job directories are private and removed on every exit path.
- qpdf exit 3 is accepted only when reconstruction creates a normalized PDF that subsequently passes a clean check.
- Raw `.f11` files are not accepted over IPP.
- The printer has no proven physical completion acknowledgement. A successful bulk transfer proves host-side transmission only.
- Keep this trusted-LAN only; do not expose CUPS port 631 to the public Internet.

## Troubleshooting

### iOS discovers the printer but cannot select it

Check that the Pi firewall allows trusted-LAN TCP 631 and UDP 5353. Then test from another LAN host:

```bash
ipptool -t ipp://PI_ADDRESS/printers/Rongta_F11 \
  /usr/share/cups/ipptool/get-printer-attributes.test
```

Also inspect duplicate Bonjour advertisements:

```bash
dns-sd -B _ipp._tcp local.
```

### `claim interface: device or resource busy`

`usblp` probably claimed interface 0:

```bash
lsusb -t
sudo modprobe -r usblp
```

The installer writes `/etc/modprobe.d/f11-no-usblp.conf` so this remains fixed after reboot.

### CUPS backend failed

```bash
lpstat -W all -o Rongta_F11
sudo tail -100 /var/log/cups/error_log
sudo tail -100 /var/log/cups/access_log
```

Preserved CUPS files can be replayed without USB:

```bash
sudo F11_DRY_RUN=1 DEVICE_URI=f11:/ /usr/lib/cups/backend/f11 \
  900 tester replay 1 '' /var/spool/cups/dNNNNN-001
```

### Recoverable PDF warning

Some iOS PDFs contain damaged xref entries such as `object has offset 0`. The backend reconstructs these with qpdf and requires the result to pass a clean second check before rendering.

## Development and tests

```bash
cd Appliance
go test ./...
go vet ./...
bash -n cups/f11 scripts/install.sh Tests/backend-integration.sh
shellcheck cups/f11 scripts/install.sh Tests/backend-integration.sh
./Tests/backend-integration.sh
```

The integration test requires qpdf and Poppler. It generates clean and repairable five-page PDFs, requires five independently validated F11 streams for each, disables USB, and verifies temporary cleanup.

Cross-builds:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/f11d-arm64 ./cmd/f11d
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -o /tmp/f11d-armv7 ./cmd/f11d
```

`testdata/swift-selftest.f11` is generated by the clean-room Swift encoder and is the language-crossing golden fixture.

## Dependencies and licensing

The appliance source is MIT-licensed with the repository. It uses `golang.org/x/image` and its transitive `golang.org/x/text` dependency under their upstream BSD-style licenses. Runtime document tools are installed from Debian repositories; no proprietary Rongta driver, filter, PPD, binary, or captured customer document is included.

## Vehicle use

Use automotive-grade regulated power and graceful shutdown/hold-up hardware. Abrupt SD-card power loss and high cabin temperatures can damage state or thermal media. The current installer does not configure a read-only root filesystem, access point, ignition sensing, or shutdown controller.
