#!/bin/bash
set -euo pipefail
export LC_ALL=C
ROOT=$(cd "$(dirname "$0")/.." && pwd)
LOCK="$ROOT/image/base-image.lock"
CACHE=${F11_IMAGE_CACHE:-"$ROOT/../.image-cache"}
DIST=${F11_IMAGE_DIST:-"$ROOT/../dist"}
LINUX_RUNNER=${F11_LINUX_RUNNER:-}
get(){ sed -n "s/^$1=//p" "$LOCK"; }
sha256(){ if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }
name=$(get BASE_IMAGE_NAME); url=$(get BASE_IMAGE_URL); want=$(get BASE_IMAGE_SHA256)
product=$(get IMAGE_PRODUCT); version=$(get IMAGE_VERSION)
[[ $want =~ ^[a-f0-9]{64}$ && -n $name && -n $product && -n $version ]] || { echo 'invalid base-image.lock' >&2; exit 1; }
install -d "$CACHE" "$DIST"
base_xz="$CACHE/$name"; base_img="$CACHE/${name%.xz}"
if [[ ! -f $base_xz ]]; then curl --fail --location --output "$base_xz.part" "$url"; mv "$base_xz.part" "$base_xz"; fi
got=$(sha256 "$base_xz" | cut -d' ' -f1)
[[ $got == "$want" ]] || { echo "base image checksum mismatch" >&2; exit 1; }
[[ -f $base_img ]] || xz -dk "$base_xz"
overlay=${F11_PREBUILT_OVERLAY:-"$CACHE/overlay-$version"}
if [[ -n ${F11_PREBUILT_OVERLAY:-} ]]; then
  [[ -s $overlay/meta/manifest.json && -s $overlay/meta/base-image.lock ]] || { echo 'invalid prebuilt overlay' >&2; exit 1; }
else
  rm -rf "$overlay"; "$ROOT/image/build-overlay.sh" "$overlay"
fi
out_img="$CACHE/$product-$version.img"
if [[ ${F11_REUSE_CUSTOM_IMAGE:-0} == 1 ]]; then
  [[ -s $out_img && -s $out_img.packages.tsv ]] || { echo 'reusable custom image/package inventory missing' >&2; exit 1; }
else
  rm -f "$out_img" "$out_img.packages.tsv"
  if [[ $(uname -s) == Linux && $EUID -eq 0 ]]; then
    "$ROOT/image/linux/customize-image.sh" "$base_img" "$overlay" "$out_img"
  elif [[ -n $LINUX_RUNNER ]]; then
    "$LINUX_RUNNER" "$base_img" "$overlay" "$out_img"
  else
    echo 'Linux image customization required. Set F11_LINUX_RUNNER or use image/macos/build-with-lima.sh.' >&2; exit 2
  fi
fi
xz -T0 -6 -c "$out_img" >"$DIST/$product-$version.img.xz"
command -v bmaptool >/dev/null || { echo 'bmaptool required after customization' >&2; exit 1; }
bmaptool create "$out_img" >"$DIST/$product-$version.bmap"
(cd "$DIST" && sha256 "$product-$version.img.xz" >"$product-$version.img.xz.sha256")
cp "$overlay/meta/manifest.json" "$DIST/$product-$version.manifest.json"
cp "$LOCK" "$DIST/$product-$version.base-image.lock"
cp "$ROOT/image/README.md" "$DIST/README.md"
install -m0755 "$ROOT/image/flash-card.py" "$DIST/flash-card.py"
cp "$ROOT/../LICENSE" "$DIST/LICENSE"
cp "$out_img.packages.tsv" "$DIST/$product-$version.packages.tsv"
if [[ -n ${F11_SOURCE_ARCHIVE:-} ]]; then
  test -s "$F11_SOURCE_ARCHIVE"
  cp "$F11_SOURCE_ARCHIVE" "$DIST/$product-$version-source.tar.gz"
elif ! git -C "$ROOT/.." diff --quiet --ignore-submodules HEAD || [[ -n $(git -C "$ROOT/.." status --porcelain --untracked-files=normal) ]]; then
  echo 'source tree must be clean before publishing release source archive' >&2; exit 1
else
  git -C "$ROOT/.." archive --format=tar.gz -o "$DIST/$product-$version-source.tar.gz" HEAD
fi
tar -tzf "$DIST/$product-$version-source.tar.gz" | grep -Fqx 'LICENSE' && tar -tzf "$DIST/$product-$version-source.tar.gz" | grep -Fqx 'Appliance/go.mod' && ! tar -tzf "$DIST/$product-$version-source.tar.gz" | grep -E '(__pycache__|\.pyc$|settings\.toml$|token\.json$|events\.jsonl$|f11-personalize\.json$)'
(cd "$DIST" && sha256 "$product-$version.img.xz" "$product-$version.bmap" "$product-$version.manifest.json" "$product-$version.packages.tsv" "$product-$version-source.tar.gz" flash-card.py >SHA256SUMS)
printf 'image=%s\n' "$DIST/$product-$version.img.xz"
printf 'sha256=%s\n' "$(cut -d' ' -f1 "$DIST/$product-$version.img.xz.sha256")"
rm -f "$out_img" "$out_img.packages.tsv"
