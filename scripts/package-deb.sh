#!/usr/bin/env bash
# 在 Linux 上把 reelkeep-desktop 打成 .deb；WebKitGTK 由系统依赖提供。
set -euo pipefail

VERSION="${1:?version}"
ARCH="${2:?arch}"
BIN="${3:-dist/reelkeep-desktop}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [ ! -f "$BIN" ]; then
  echo "missing binary: $BIN" >&2
  exit 1
fi

STAGE="$ROOT/dist/deb-root"
rm -rf "$STAGE"
mkdir -p "$STAGE/DEBIAN" \
         "$STAGE/usr/bin" \
         "$STAGE/usr/share/applications" \
         "$STAGE/usr/share/icons/hicolor/256x256/apps"

install -m 0755 "$BIN" "$STAGE/usr/bin/reelkeep-desktop"
install -m 0644 "$ROOT/packaging/linux/reelkeep.desktop" "$STAGE/usr/share/applications/reelkeep.desktop"
install -m 0644 "$ROOT/desktop/icon.png" "$STAGE/usr/share/icons/hicolor/256x256/apps/reelkeep.png"

sed -e "s/__VERSION__/$VERSION/g" -e "s/__ARCH__/$ARCH/g" \
  "$ROOT/packaging/linux/control" > "$STAGE/DEBIAN/control"
chmod 0644 "$STAGE/DEBIAN/control"

OUT="$ROOT/dist/reelkeep_${VERSION}_${ARCH}.deb"
dpkg-deb --build --root-owner-group "$STAGE" "$OUT" >/dev/null
rm -rf "$STAGE"
echo "built $OUT"
