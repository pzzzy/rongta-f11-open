# Third-party components

The repository does not commit third-party binaries or source trees. The optional AirPrint installer fetches and builds the following open-source dependency into a user-local prefix.

## ISTO Printer Working Group IPP Sample Implementations

- Project: `istopwg/ippsample`
- Source: https://github.com/istopwg/ippsample
- Tested/pinned commit: `ce11296293da30a111615a2deddaea51268b49ce`
- License: Apache License 2.0
- Installed prefix: `~/.local/ippsample`

The project includes its own pinned libcups and PDFio submodules and their corresponding license files. Those upstream notices remain in the fetched source and installed documentation.

The F11 bridge applies three checked build-time corrections to the pinned source:

- Correct `printer-resolutions-supported` to the actual IPP attribute name `printer-resolution-supported`, preventing a duplicate 600-dpi capability alongside the F11's actual 203 dpi.
- Change the reference server's default active-job admission limit from 100 to 1, bounding concurrent network spool growth.
- Restrict the generated media and media-size tables to US Letter, matching the implemented backend.

The installer requires each original source fragment to occur exactly once and fails closed if the pinned source differs. The installed provenance marker records both the upstream commit and local patch-set version (`f11patch2`).

The bridge also builds only the native arm64 slice on Apple Silicon because Homebrew dependencies on such systems are arm64-only, while the upstream macOS default requests a universal arm64/x86_64 build.

No upstream source or binary is copied into this Git repository.

## libusb

- Project: libusb
- Source: https://libusb.info/
- License: LGPL-2.1-or-later

The application build links against the user's Homebrew installation. No libusb binary is committed to this repository. Generated local application bundles include the applicable libusb license notice.
