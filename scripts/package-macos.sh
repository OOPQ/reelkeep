#!/usr/bin/env bash
# 在 macOS 上把 reelkeep-desktop 打成 Reelkeep.app + dmg。
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

APP="$ROOT/dist/Reelkeep.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

install -m 0755 "$BIN" "$APP/Contents/MacOS/reelkeep-desktop"
sed "s/__VERSION__/$VERSION/g" "$ROOT/packaging/macos/Info.plist" > "$APP/Contents/Info.plist"

ICONSET="$ROOT/dist/AppIcon.iconset"
rm -rf "$ICONSET"
mkdir -p "$ICONSET"
SRC="$ROOT/desktop/icon.png"
for spec in "16:16x16" "32:16x16@2x" "32:32x32" "64:32x32@2x" \
            "128:128x128" "256:128x128@2x" "256:256x256" "512:256x256@2x" \
            "512:512x512" "1024:512x512@2x"; do
  size="${spec%%:*}"
  name="${spec##*:}"
  sips -z "$size" "$size" "$SRC" --out "$ICONSET/icon_${name}.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/icon.icns"
rm -rf "$ICONSET"

# ad-hoc 签名：未公证，但 arm64 上至少能正常启动
codesign --force --deep --sign - "$APP" >/dev/null
codesign --verify --deep --strict "$APP"

OUT_DMG="$ROOT/dist/Reelkeep-$VERSION-macos-$ARCH.dmg"
rm -f "$OUT_DMG"
# hdiutil 在 CI 上偶发 "Resource busy"/"Invalid argument"，重试几次。
for attempt in 1 2 3; do
  if hdiutil create -volname "Reelkeep" -srcfolder "$APP" -ov -format UDZO "$OUT_DMG" >/dev/null; then
    break
  fi
  echo "hdiutil create failed (attempt $attempt)，重试" >&2
  rm -f "$OUT_DMG"
  if [ "$attempt" = 3 ]; then
    exit 1
  fi
  sleep 3
done

tar -czf "$ROOT/dist/Reelkeep-$VERSION-macos-$ARCH.app.tar.gz" -C "$ROOT/dist" Reelkeep.app
echo "built $OUT_DMG"
