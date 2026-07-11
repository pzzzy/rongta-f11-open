#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$PWD
APP="$ROOT/dist/F11 PDF Printer.app"

if command -v brew >/dev/null 2>&1; then
  LIBUSB_PREFIX=$(brew --prefix libusb)
else
  echo "Homebrew libusb is required to build the USB-enabled app bundle." >&2
  exit 1
fi

rm -rf "$APP" "$ROOT/dist/f11print"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources" "$APP/Contents/Frameworks" "$ROOT/dist"

swift build -c release --product f11print
cp .build/release/f11print "$APP/Contents/MacOS/f11print"

clang Helpers/f11usb.c \
  -I"$LIBUSB_PREFIX/include/libusb-1.0" \
  -L"$LIBUSB_PREFIX/lib" -lusb-1.0 -O2 \
  -Wall -Wextra -Werror \
  -o "$APP/Contents/Resources/f11usb"

cp "$LIBUSB_PREFIX/lib/libusb-1.0.0.dylib" "$APP/Contents/Frameworks/"
install_name_tool -id @rpath/libusb-1.0.0.dylib "$APP/Contents/Frameworks/libusb-1.0.0.dylib"
install_name_tool -change "$LIBUSB_PREFIX/lib/libusb-1.0.0.dylib" @rpath/libusb-1.0.0.dylib "$APP/Contents/Resources/f11usb"
install_name_tool -add_rpath @loader_path/../Frameworks "$APP/Contents/Resources/f11usb"

LICENSE_SOURCE=""
for candidate in "$LIBUSB_PREFIX/share/doc/libusb/COPYING" "$LIBUSB_PREFIX/COPYING"; do
  if [[ -f "$candidate" ]]; then LICENSE_SOURCE=$candidate; break; fi
done
if [[ -n "$LICENSE_SOURCE" ]]; then
  cp "$LICENSE_SOURCE" "$APP/Contents/Resources/libusb-LICENSE.txt"
else
  printf '%s\n' 'libusb is licensed under LGPL-2.1-or-later: https://github.com/libusb/libusb' > "$APP/Contents/Resources/libusb-LICENSE.txt"
fi

cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>f11print</string>
<key>CFBundleIdentifier</key><string>org.openf11.pdfprinter</string>
<key>CFBundleName</key><string>F11 PDF Printer</string>
<key>CFBundleDisplayName</key><string>F11 PDF Printer</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>1.0.0</string>
<key>CFBundleVersion</key><string>1</string>
<key>LSMinimumSystemVersion</key><string>13.0</string>
<key>NSHighResolutionCapable</key><true/>
<key>CFBundleDocumentTypes</key><array><dict>
<key>CFBundleTypeName</key><string>PDF Document</string>
<key>CFBundleTypeRole</key><string>Viewer</string>
<key>LSItemContentTypes</key><array><string>com.adobe.pdf</string></array>
</dict></array>
</dict></plist>
PLIST

codesign --force --sign - "$APP/Contents/Frameworks/libusb-1.0.0.dylib"
codesign --force --sign - "$APP/Contents/Resources/f11usb"
codesign --force --deep --sign - "$APP"
ln -s "F11 PDF Printer.app/Contents/MacOS/f11print" "$ROOT/dist/f11print"

plutil -lint "$APP/Contents/Info.plist"
codesign --verify --deep --strict "$APP"
if find "$APP" -type f \( -iname '*.ppd' -o -iname '*rongta*' -o -name 'f11raster' \) | grep -q .; then
  echo "Proprietary or legacy artifact detected in app bundle." >&2
  exit 1
fi
printf '%s\n' "$APP"
