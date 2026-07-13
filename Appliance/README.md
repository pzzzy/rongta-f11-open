# Raspberry Pi AirPrint appliance

Linux-native clean-room Rongta F11 support for Raspberry Pi CUPS/AirPrint. Physically developed on Raspberry Pi 4, Debian 13 arm64, USB `0fe6:811e`; the protocol core also cross-builds for ARMv7.

## Architecture

```text
iPhone/macOS
  -> Bonjour + IPP/IPPS
  -> CUPS document normalization
  -> unprivileged pdftof11 filter
  -> qpdf validation + bounded Ghostscript 203-dpi grayscale raster
  -> clean-room Go F11 encoder
  -> strict independent decode/row comparison
  -> complete validated RTProtocol job on stdout
  -> standard CUPS usb backend (libusb, unidir, delay-close)
  -> Rongta F11
```

The filter never opens USB. CUPS owns discovery, serialization, USB interface detach/reattach, synchronous bulk transfers, and job lifetime. A narrow local CUPS quirk for `0fe6:811e` enables `unidir delay-close`, preventing the final transfer from being lost during interface release.

## Clean-room boundary

The encoder, decoder, renderer, filter, validation, and installer are independently authored. Protocol interoperability research used observed official-driver output and controlled differential fixtures as documented in `../Docs/PROVENANCE.md`; those captures are not distributed. Later static package inspection corroborated the filter-to-stdout/CUPS-backend architecture and declarative printer properties. No proprietary binary, PPD, disassembly, or copied vendor source is included or required at runtime.

## Install

```bash
git clone https://github.com/pzzzy/rongta-f11-open.git
cd rongta-f11-open/Appliance
sudo ./scripts/install.sh
```

The installer:

- installs maintained Debian CUPS, Avahi, qpdf, Poppler, Ghostscript, and Go packages;
- runs tests and vet, then builds `f11d` and `bannerprint` locally;
- installs the unprivileged PDF-to-F11 stdout filter and clean-room PPD;
- discovers exactly one F11 using `/usr/lib/cups/backend/usb` and pins its discovered URI, including serial when supplied by CUPS;
- refuses ambiguous hardware or an unrelated existing `Rongta_F11` queue;
- places a nonprinting migration-hold backend before starting CUPS, disables the legacy queue, cancels outstanding legacy jobs through CUPS, updates the queue in place, then removes the hold and enables printing;
- installs the narrow `0x0fe6 0x811e unidir delay-close` CUPS USB quirk;
- removes obsolete `usblp` blacklists while leaving kernel-driver detach/reattach to CUPS;
- installs a nonprinting health check and shares `Rongta_F11` over AirPrint.

Firewall changes are opt-in:

```bash
sudo ./scripts/install.sh --configure-nftables
```

## Huge text banners

`bannerprint` renders a full-width, approximately 15-inch-long roll banner, independently validates the complete F11 stream, and submits exactly one raw job through the configured CUPS queue. It never opens USB directly.

```bash
bannerprint "SYSTEM OFFLINE"
bannerprint --lines 1 "ONE LARGE LINE"
bannerprint --lines 2 "PLEASE USE OTHER DOOR"
bannerprint --lines 3 "MEETING IN PROGRESS PLEASE WAIT"
bannerprint --font comic-sans --lines 2 "NO PARKING"
bannerprint --preview --lines auto "INSPECT WITHOUT PRINTING"
```

- `--lines auto` (default) evaluates one, two, and three contiguous word-preserving lines and chooses the largest type.
- `--lines 1`, `2`, or `3` forces that exact line count; there must be at least one word per line.
- `--font bold` is the default embedded Go Bold face.
- `--font comic-sans` uses embedded Comic Neue Bold, an open-source Comic Sans-style face under SIL OFL 1.1. Microsoft Comic Sans is not distributed.
- `--preview` performs full layout, render, encode, independent decode, copy-count verification, and raster comparison without calling CUPS or printing.
- Input is bounded to 256 valid UTF-8 bytes and 16 words. Control characters, combining marks, and glyphs unsupported by the selected face are rejected before CUPS. The accepted 16-word worst case completed in 4 seconds on the original single-core Pi Zero W under a 30-second hard test deadline.
- Copies are forced to one in both the validated F11 stream and `lp -n 1`. Before submission, the queue URI and attached USB device are checked with the same serial-aware F11 runtime identity verifier used by appliance health.
- The validated stream is piped to `lp` over stdin, so no temporary banner document remains after success, error, or interruption. Set `F11_QUEUE` or use `--queue NAME` only when targeting a different local F11 CUPS queue.

## Diagnostics (never print)

```bash
/usr/local/lib/f11/f11d self-test
/usr/local/lib/f11/f11-health
/usr/lib/cups/backend/usb
lpstat -t
ipptool -t ipp://localhost/printers/Rongta_F11 \
  /usr/share/cups/ipptool/get-printer-attributes.test
```

Expected protocol self-test:

```text
bytes:  913
rows:   8
sha256: bffe45513da30e7fc29b4e404154cb65a87637a1df1951929fa49f248f4627f4
```

Standalone raw USB sending is intentionally not installed. CUPS is the sole production writer.

## Limits and safety

- Normalized PDF: at most 64 MiB.
- Pages: 1–20.
- Raster: native width 1,664 dots; page height is preserved at 203 dpi and bounded to 20–2,233 rows.
- Media presets: F11 Short (32 mm, default), borderless 4×6, 5×7, A5, 8×10, and US Letter. The 4×6, 5×7, A5, and 8×10 presets use true 203-dpi logical canvases centered on the fixed 1,664-dot head. Letter is compatibility media: its 8.5-inch width exceeds the approximately 8.197-inch head, so it is uniformly fitted to the maximum printable width without distortion.
- For landscape source pages, 4×6 and 5×7 logical canvases rotate when their long edge still fits the 1,664-dot head. Larger presets remain portrait because their landscape width exceeds the physical head. PDF `/Rotate` metadata is included in the effective page orientation.
- Rendering uses uniform aspect-preserving fit with centered white padding. iOS decides which generic scaling controls each source app exposes; the driver does not claim a custom zoom slider or unverified fill/crop behavior.
- Copies: expanded by the filter only; each encoded protocol job has one copy.
- Every page/copy is staged and fully validated before the first byte reaches filter stdout. A later-page failure emits no partial printer stream.
- Temporary directories are private and removed on all normal/error exits.
- qpdf status 3 is accepted only if the reconstructed PDF subsequently passes a clean check.
- Raw `.f11` files are not accepted over IPP.
- A successful USB transfer means transport completion, not proven mechanical completion/ejection; the F11 has no established per-job acknowledgement.
- Blank raster rows are not treated as a proven paper-feed command.
- Keep CUPS on a trusted LAN; never expose TCP 631 to the public Internet.
- Physical regression patterns should use only 10–30 mm of low-coverage paper and include a distinct final marker.

## Troubleshooting

### USB discovery

```bash
sudo /usr/lib/cups/backend/usb
lsusb -t
sudo tail -100 /var/log/cups/error_log
```

The backend should report exactly one F11 `usb:` URI. CUPS may detach and reattach `usblp` while owning a print job; this is normal.

### Queue and AirPrint

```bash
lpstat -W all -o Rongta_F11
lpstat -v Rongta_F11
dns-sd -B _ipp._tcp local.
ipptool -t ipp://PI_ADDRESS/printers/Rongta_F11 \
  /usr/share/cups/ipptool/get-printer-attributes.test
```

### Offline filter validation

The integration test captures filter stdout to a file and never invokes USB:

```bash
cd Appliance
./Tests/filter-integration.sh
```

It generates a three-page PDF with two copies, splits six concatenated RTProtocol jobs, strictly validates each one, forces a page-three failure, proves that failure emits zero bytes, and verifies cleanup.

## Development

```bash
cd Appliance
go test ./...
go test -race ./...
go vet ./...
python3 Tests/filter-architecture.py
python3 Tests/queue-migration.py
python3 Tests/firewall-helper.py
bash -n cups/pdftof11 cups/f11-migration-hold scripts/f11-health.sh \
  scripts/install.sh Tests/filter-integration.sh
shellcheck cups/pdftof11 cups/f11-migration-hold scripts/f11-health.sh \
  scripts/install.sh Tests/filter-integration.sh
./Tests/filter-integration.sh
```

Cross-builds:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/f11d-arm64 ./cmd/f11d
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -o /tmp/f11d-armv7 ./cmd/f11d
```

`testdata/swift-selftest.f11` is the clean-room Swift/Go golden fixture.

## Licensing

MIT licensed. Go dependencies retain their upstream BSD-style licenses. Runtime document tools come from Debian repositories. No proprietary Rongta component is redistributed.
