#!/bin/sh
set -eu

ROOTFS=${1:-/mnt/UDISK/ubuntu-portmaster-rootfs}

# Reverse dependency order. Busy mounts are reported and left intact.
for target in \
    "$ROOTFS/roms/ports/PortMaster" \
    "$ROOTFS/roms/ports" \
    "$ROOTFS/usr/trimui/lib" \
    "$ROOTFS/opt/trimui-host/usr/lib" \
    "$ROOTFS/etc/openwrt_release" \
    "$ROOTFS/etc/version" \
    "$ROOTFS/mnt/SDCARD" \
    "$ROOTFS/host" \
    "$ROOTFS/dev/pts" \
    "$ROOTFS/dev" \
    "$ROOTFS/sys" \
    "$ROOTFS/proc" \
    "$ROOTFS/tmp"
do
    if grep -qs " $target " /proc/mounts; then
        umount "$target" || echo "Still busy: $target" >&2
    fi
done
