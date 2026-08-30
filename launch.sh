#!/bin/sh
APP_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$APP_DIR" || exit 1
exec "$APP_DIR/trimui-chroot-manager"
