#!/usr/bin/env bash
set -euo pipefail
umask 077
cd "$(dirname "$0")/.."
PREFIX=${F11_AIRPRINT_PREFIX:-$HOME/.local/rongta-f11}
PORT=${F11_AIRPRINT_PORT:-8631}
IPPSAMPLE_PREFIX=${F11_AIRPRINT_IPPSAMPLE:-$HOME/.local/ippsample}
IPPSAMPLE_SOURCE=${F11_AIRPRINT_IPPSAMPLE_SOURCE:-$HOME/src/ippsample}
IPPSAMPLE_COMMIT=${F11_AIRPRINT_IPPSAMPLE_COMMIT:-ce11296293da30a111615a2deddaea51268b49ce}
IPPSAMPLE_BUILD_ID="$IPPSAMPLE_COMMIT-f11patch2"
STATE="$HOME/Library/Application Support/F11AirPrint"
AGENT="$HOME/Library/LaunchAgents/com.pzzzy.f11-airprint.plist"
UID_VALUE=$(id -u)

build_ippsample() {
  mkdir -p "$(dirname "$IPPSAMPLE_SOURCE")"
  if [ ! -d "$IPPSAMPLE_SOURCE/.git" ]; then
    git clone --recursive https://github.com/istopwg/ippsample.git "$IPPSAMPLE_SOURCE"
  fi
  git -C "$IPPSAMPLE_SOURCE" fetch --tags origin
  git -C "$IPPSAMPLE_SOURCE" checkout --detach "$IPPSAMPLE_COMMIT"
  git -C "$IPPSAMPLE_SOURCE" reset --hard "$IPPSAMPLE_COMMIT"
  git -C "$IPPSAMPLE_SOURCE" clean -fdx
  git -C "$IPPSAMPLE_SOURCE" submodule update --init --recursive --force
  git -C "$IPPSAMPLE_SOURCE" submodule foreach --recursive 'git reset --hard && git clean -fdx'
  # Apply checked, pinned-source corrections: exact resolution attribute, one admitted job,
  # and Letter-only media. Fail if upstream changes instead of silently patching unknown source.
  python3 - "$IPPSAMPLE_SOURCE" <<'PY'
from pathlib import Path
import sys
root=Path(sys.argv[1])
changes={
 root/'server/printer.c': [
  ('"printer-resolutions-supported"','"printer-resolution-supported"',1),
  ('''  static const int media_col_sizes[][2] =
  {\t\t\t\t\t/* Default media-col sizes */
    { 21590, 27940 },\t\t\t/* Letter */
    { 21590, 35560 },\t\t\t/* Legal */
    { 21000, 29700 }\t\t\t/* A4 */
  };''','''  static const int media_col_sizes[][2] =
  {\t\t\t\t\t/* F11 Letter-only media size */
    { 21590, 27940 }\t\t\t/* Letter */
  };''',1),
  ('''  static const char * const media_supported[] =
  {\t\t\t\t\t/* Default media-supported values */
    "na_letter_8.5x11in",\t\t/* Letter */
    "na_legal_8.5x14in",\t\t/* Legal */
    "iso_a4_210x297mm"\t\t\t/* A4 */
  };''','''  static const char * const media_supported[] =
  {\t\t\t\t\t/* F11 Letter-only media */
    "na_letter_8.5x11in"\t\t/* Letter */
  };''',1),
 ],
 root/'server/ippserver.h': [('MaxJobs\t\tVALUE(100)','MaxJobs\t\tVALUE(1)',1)],
}
for path,repls in changes.items():
 text=path.read_text()
 for old,new,count in repls:
  actual=text.count(old)
  if actual != count:
   raise SystemExit(f'{path}: expected {count} occurrence(s), found {actual}')
  text=text.replace(old,new,count)
 path.write_text(text)
PY
  (cd "$IPPSAMPLE_SOURCE" && ./configure --prefix="$IPPSAMPLE_PREFIX")
  # Homebrew libraries on Apple Silicon are arm64-only; upstream defaults to a universal macOS build.
  python3 - "$IPPSAMPLE_SOURCE" <<'PY'
from pathlib import Path
import sys
root=Path(sys.argv[1])
for p in (root/'Makedefs',root/'libcups/Makedefs',root/'libcups/pdfio/Makefile'):
    text=p.read_text().replace('-arch x86_64 -arch arm64','-arch arm64')
    p.write_text(text)
PY
  (cd "$IPPSAMPLE_SOURCE" && make clean >/dev/null 2>&1 || true && make -j"$(sysctl -n hw.ncpu)" && make install)
  printf '%s\n' "$IPPSAMPLE_BUILD_ID" > "$IPPSAMPLE_PREFIX/.f11-airprint-ippsample-commit"
}

INSTALLED_COMMIT=$(cat "$IPPSAMPLE_PREFIX/.f11-airprint-ippsample-commit" 2>/dev/null || true)
if [ ! -x "$IPPSAMPLE_PREFIX/sbin/ippserver" ] || [ ! -x "$IPPSAMPLE_PREFIX/bin/ipptransform" ] || [ "$INSTALLED_COMMIT" != "$IPPSAMPLE_BUILD_ID" ]; then
  build_ippsample
fi

# Give all locally built tools and their shared library a consistent ad-hoc identity.
for f in "$IPPSAMPLE_PREFIX"/lib/*.dylib "$IPPSAMPLE_PREFIX"/bin/* "$IPPSAMPLE_PREFIX"/sbin/*; do
  [ -f "$f" ] && codesign --force --sign - "$f" >/dev/null
 done

./Scripts/build-app.sh >/dev/null
mkdir -p "$PREFIX/bin" "$PREFIX/libexec" "$PREFIX/share/f11-airprint" "$STATE/logs"
install -m 755 dist/f11print "$PREFIX/bin/f11print"
install -m 755 "dist/F11 PDF Printer.app/Contents/Resources/f11usb" "$PREFIX/libexec/f11usb"
install -m 755 Scripts/f11-airprint-backend.sh "$PREFIX/libexec/f11-airprint-backend"
install -m 755 Scripts/run-airprint-bridge.sh "$PREFIX/bin/f11-airprint"
install -m 644 Config/printer.attrs "$PREFIX/share/f11-airprint/printer.attrs"

# The standalone CLI resolves f11usb relative to ../Resources; provide that layout without duplicating bytes.
mkdir -p "$PREFIX/Resources"
ln -sf ../libexec/f11usb "$PREFIX/Resources/f11usb"

mkdir -p "$(dirname "$AGENT")"
python3 - "$AGENT" "$PREFIX" "$IPPSAMPLE_PREFIX" "$STATE" "$PORT" <<'PY'
import plistlib,sys
agent,prefix,ippsample,state,port=sys.argv[1:]
plist={
 'Label':'com.pzzzy.f11-airprint',
 'ProgramArguments':['/usr/bin/env','-i',
   f'HOME={__import__("os").path.expanduser("~")}',
   'PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin',
   f'F11_AIRPRINT_PREFIX={prefix}',f'F11_AIRPRINT_IPPSAMPLE={ippsample}',
   f'F11_AIRPRINT_PORT={port}',f'{prefix}/bin/f11-airprint'],
 'RunAtLoad':True,'KeepAlive':True,'ProcessType':'Background',
 'StandardOutPath':f'{state}/logs/stdout.log',
 'StandardErrorPath':f'{state}/logs/stderr.log',
}
with open(agent,'wb') as f: plistlib.dump(plist,f,sort_keys=False)
PY
plutil -lint "$AGENT"
launchctl bootout "gui/$UID_VALUE/com.pzzzy.f11-airprint" >/dev/null 2>&1 || true
for _ in $(seq 1 50); do
  if ! launchctl print "gui/$UID_VALUE/com.pzzzy.f11-airprint" >/dev/null 2>&1; then break; fi
  sleep 0.1
done
bootstrapped=0
for _ in $(seq 1 20); do
  if launchctl bootstrap "gui/$UID_VALUE" "$AGENT"; then bootstrapped=1; break; fi
  sleep 0.25
done
[ "$bootstrapped" = 1 ] || { echo "could not register AirPrint LaunchAgent" >&2; exit 1; }
launchctl enable "gui/$UID_VALUE/com.pzzzy.f11-airprint"
launchctl kickstart -k "gui/$UID_VALUE/com.pzzzy.f11-airprint"
echo "$PREFIX"
