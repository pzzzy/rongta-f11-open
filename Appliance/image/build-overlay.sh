#!/bin/bash
set -euo pipefail
export LC_ALL=C
umask 022
ROOT=$(cd "$(dirname "$0")/.." && pwd)
LOCK="$ROOT/image/base-image.lock"
OUT=${1:-"$ROOT/../dist/overlay"}
OUT=$(python3 -c 'import os,sys; print(os.path.abspath(sys.argv[1]))' "$OUT")
[[ -f $LOCK ]] || { echo 'missing base-image.lock' >&2; exit 1; }
if find "$ROOT/image/rootfs" \( -type d -name __pycache__ -o -type f \( -name '*.pyc' -o -name '*.pyo' \) \) -print -quit | grep -q .; then
  echo 'generated Python cache/bytecode found in image rootfs source' >&2
  exit 1
fi
rm -rf "$OUT"
install -d -m0755 "$OUT/rootfs" "$OUT/bootfs" "$OUT/meta" "$OUT/rootfs/usr/local/bin" "$OUT/rootfs/usr/local/lib/f11" "$OUT/rootfs/usr/share/f11-image" "$OUT/rootfs/etc/systemd/system/multi-user.target.wants"
cp -a "$ROOT/image/rootfs/." "$OUT/rootfs/"
for executable in \
  usr/local/lib/f11-image/first-boot \
  usr/local/lib/f11-image/import-envelope \
  usr/local/lib/f11-image/import-settings \
  usr/local/lib/f11-image/install-appliance \
  usr/local/lib/f11-image/install-twitch-authorization \
  usr/local/lib/f11-image/network-recover \
  usr/local/lib/f11-image/verify-eventsub \
  usr/local/lib/f11/provision-printer \
  usr/local/lib/f11/print-led; do
  [[ -f $OUT/rootfs/$executable ]] || { echo "missing executable $executable" >&2; exit 1; }
  chmod 0755 "$OUT/rootfs/$executable"
done

build() { local pkg=$1 name=$2; CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -trimpath -ldflags='-s -w' -o "$OUT/rootfs/usr/local/bin/$name" "$pkg"; }
cd "$ROOT"
build ./cmd/f11d f11d
install -m0755 "$OUT/rootfs/usr/local/bin/f11d" "$OUT/rootfs/usr/local/lib/f11/f11d"
build ./cmd/bannerprint bannerprint
build ./cmd/twitch-banner twitch-banner
build ./cmd/giftprint giftprint
build ./cmd/raidprint raidprint
for optional in f11-setup-wizard f11-support f11-setup-helper; do
  if [[ -f cmd/$optional/main.go ]]; then build "./cmd/$optional" "$optional"; fi
done
install -m0755 scripts/check-f11-runtime.py "$OUT/rootfs/usr/local/lib/f11/check-f11-runtime"
install -m0755 scripts/plan-queue-migration.py "$OUT/rootfs/usr/local/lib/f11/plan-queue-migration"
install -m0755 scripts/pdf-page-height.py "$OUT/rootfs/usr/local/lib/f11/pdf-page-height"
install -m0755 scripts/media-canvas.py "$OUT/rootfs/usr/local/lib/f11/media-canvas"
install -m0755 scripts/f11-health.sh "$OUT/rootfs/usr/local/lib/f11/f11-health"
install -m0755 cups/pdftof11 "$OUT/rootfs/usr/local/lib/f11/pdftof11"
install -m0644 cups/f11.ppd "$OUT/rootfs/usr/share/f11-image/f11.ppd"
install -m0644 cups/0fe6-811e.usb-quirks "$OUT/rootfs/usr/share/f11-image/0fe6-811e.usb-quirks"
install -m0644 systemd/twitch-banner.service "$OUT/rootfs/usr/share/f11-image/twitch-banner.service"
install -m0644 systemd/f11-health.service "$OUT/rootfs/usr/share/f11-image/f11-health.service"
install -m0644 systemd/f11-print-led.service "$OUT/rootfs/usr/share/f11-image/f11-print-led.service"
for u in f11-setup-wizard.service f11-setup-helper.service; do [[ -f systemd/$u ]] && install -m0644 "systemd/$u" "$OUT/rootfs/usr/share/f11-image/$u"; done
cp "$LOCK" "$OUT/meta/base-image.lock"
git -C "$ROOT/.." rev-parse HEAD >"$OUT/meta/source-commit"
printf '0.1.0-rc1+%s\n' "$(cat "$OUT/meta/source-commit" | cut -c1-12)" >"$OUT/rootfs/usr/share/f11-image/version"
printf 'Rongta F11 Twitch Printer Appliance\n' >"$OUT/rootfs/etc/f11-image-release"
ln -s ../f11-first-boot.service "$OUT/rootfs/etc/systemd/system/multi-user.target.wants/f11-first-boot.service"
python3 - "$OUT" <<'PY'
import hashlib,json,os,pathlib,sys
root=pathlib.Path(sys.argv[1]).resolve(); files=[]
for p in sorted(root.rglob('*')):
    if p.name == '__pycache__' or p.suffix in {'.pyc','.pyo'}: raise SystemExit(f'generated Python cache/bytecode in overlay: {p.relative_to(root)}')
    if p.is_file() and p.name not in {'SHA256SUMS','manifest.json'}:
        b=p.read_bytes(); files.append({'path':str(p.relative_to(root)),'bytes':len(b),'sha256':hashlib.sha256(b).hexdigest(),'mode':oct(p.stat().st_mode & 0o777)})
(root/'meta/manifest.json').write_text(json.dumps({'schema':1,'target':'linux/arm/v6','files':files},indent=2)+'\n')
(root/'meta/SHA256SUMS').write_text(''.join(f"{item['sha256']}  {item['path']}\n" for item in files))
PY
python3 - "$OUT" <<'PY'
import os,pathlib,re,sys
root=pathlib.Path(sys.argv[1])
for p in (root/'meta').iterdir():
    if p.is_file() and any(x in p.read_text(errors='ignore') for x in ('/Users/','/workspace/','.image-cache/')): raise SystemExit('host path leaked into overlay metadata')
forbidden_names={'.env','settings.toml','token.json','events.jsonl','authorized_keys','id_rsa','id_ed25519'}
canary=os.environ.get('F11_SECRET_CANARY','')
problems=[]
for p in root.rglob('*'):
    if not p.is_file(): continue
    rel=str(p.relative_to(root))
    if p.name in forbidden_names or p.suffix in {'.key','.p12','.pfx'}: problems.append(rel+': forbidden secret-bearing filename')
    data=p.read_bytes()
    if canary and canary.encode() in data: problems.append(rel+': secret canary leaked')
    if b'-----BEGIN OPENSSH PRIVATE KEY-----' in data or b'-----BEGIN PRIVATE KEY-----' in data: problems.append(rel+': private key')
    try: text=data.decode()
    except UnicodeDecodeError: continue
    for n,line in enumerate(text.splitlines(),1):
        if re.search(r'(?i)^\s*(TWITCH_CLIENT_SECRET|TWITCH_ACCESS_TOKEN|TWITCH_REFRESH_TOKEN|WIFI_PASSWORD|PSK)\s*=\s*[^\s$"\'\{][^#]*$',line):
            problems.append(f'{rel}:{n}: literal credential assignment')
if problems:
    print('\n'.join(problems),file=sys.stderr); raise SystemExit(1)
PY
printf 'overlay=%s files=%s\n' "$OUT" "$(find "$OUT" -type f | wc -l | tr -d ' ')"
