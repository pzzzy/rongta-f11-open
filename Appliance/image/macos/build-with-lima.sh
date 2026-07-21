#!/bin/bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
NAME=f11-image-builder
REPO=$(basename "$(cd "$ROOT/.." && pwd)")
command -v limactl >/dev/null || { echo 'Install Lima: brew install lima' >&2; exit 1; }
if ! limactl list --format '{{.Name}}' 2>/dev/null | grep -Fqx "$NAME"; then
  limactl create --name "$NAME" "$ROOT/image/macos/lima.yaml"
fi
overlay="$ROOT/../.image-cache/overlay-host"
rm -rf "$overlay"
"$ROOT/image/build-overlay.sh" "$overlay"
source_archive="$ROOT/../.image-cache/source-host.tar.gz"
git -C "$ROOT/.." diff --quiet --ignore-submodules HEAD || { echo 'source tree must be clean before image release' >&2; exit 1; }
[[ -z $(git -C "$ROOT/.." status --porcelain --untracked-files=normal) ]] || { echo 'source tree has untracked files' >&2; exit 1; }
git -C "$ROOT/.." archive --format=tar.gz -o "$source_archive" HEAD
limactl start "$NAME"
limactl shell --workdir "/workspace/$REPO" "$NAME" sudo bash -lc \
  "cd /workspace/$REPO/Appliance && F11_PREBUILT_OVERLAY=/workspace/$REPO/.image-cache/overlay-host F11_SOURCE_ARCHIVE=/workspace/$REPO/.image-cache/source-host.tar.gz F11_IMAGE_CACHE=/var/tmp/f11-image-cache F11_IMAGE_DIST=/workspace/$REPO/dist ./image/build-image.sh"
printf 'Artifacts: %s\n' "$(cd "$ROOT/.." && pwd)/dist"

# Stop later with: limactl stop f11-image-builder
# Delete later with: limactl delete f11-image-builder
