#!/bin/bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")" && pwd)
BUILD="$ROOT/build"
APP="$BUILD/UbuntuPortMaster"

rm -rf "$BUILD"
mkdir -p "$APP/scripts"

GOEXPERIMENT=simd CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -ldflags='-s -w' -o "$APP/trimui-chroot-manager" "$ROOT"

cp "$ROOT/config.json" "$APP/config.json"
cp "$ROOT/launch.sh" "$APP/launch.sh"
cp "$ROOT/assets/icon.png" "$APP/icon.png"
cp "$ROOT/scripts/"*.sh "$APP/scripts/"
chmod 0755 "$APP/launch.sh" "$APP/trimui-chroot-manager" "$APP/scripts/"*.sh

cd "$BUILD"
rm -f "$ROOT/UbuntuPortMaster.zip"
zip -r9 "$ROOT/UbuntuPortMaster.zip" UbuntuPortMaster
echo "Built: $ROOT/UbuntuPortMaster.zip"
