#!/bin/sh
set -eu

# Idempotently prepare an Ubuntu 24.04 arm64 rootfs for TrimUI PortMaster.
# Usage: ./trimui-ubuntu-chroot-setup.sh [ROOTFS] [ROOTFS_TARBALL]
# ROOTFS_TARBALL is required only when ROOTFS has not already been extracted.

ROOTFS=${1:-/mnt/UDISK/ubuntu-portmaster-rootfs}
ROOTFS_TARBALL=${2:-}
SDCARD=${SDCARD:-/mnt/SDCARD}
UBUNTU_PORTS_MIRROR=${UBUNTU_PORTS_MIRROR:-http://mirrors.aliyun.com/ubuntu-ports/}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PM_APP=${PM_APP:-$SDCARD/Apps/UbuntuPortMasterRuntime}

[ "$(id -u)" = 0 ] || {
    echo "Run this script as root on the TrimUI host." >&2
    exit 1
}

case "$ROOTFS" in
    ""|/) echo "Unsafe rootfs path: $ROOTFS" >&2; exit 1 ;;
esac

if [ ! -x "$ROOTFS/bin/bash" ]; then
    [ -n "$ROOTFS_TARBALL" ] && [ -f "$ROOTFS_TARBALL" ] || {
        echo "Missing rootfs. Pass an Ubuntu 24.04 arm64 rootfs tarball." >&2
        exit 1
    }
    if [ -d "$ROOTFS" ] && [ -n "$(ls -A "$ROOTFS" 2>/dev/null)" ]; then
        echo "Refusing to extract into non-empty non-rootfs directory: $ROOTFS" >&2
        exit 1
    fi

    rootfs_parent=$(dirname -- "$ROOTFS")
    rootfs_name=$(basename -- "$ROOTFS")
    extract_dir="$rootfs_parent/.${rootfs_name}.extracting-$$"
    [ ! -e "$extract_dir" ] || {
        echo "Temporary extraction directory already exists: $extract_dir" >&2
        exit 1
    }
    mkdir -p "$rootfs_parent" "$extract_dir"
    cleanup_extract() { rm -rf -- "$extract_dir"; }
    trap cleanup_extract EXIT HUP INT TERM

    case "$ROOTFS_TARBALL" in
        *.tar.gz|*.tgz)
            echo "Checking gzip archive: $ROOTFS_TARBALL"
            gzip -t "$ROOTFS_TARBALL"
            echo "Extracting gzip-compressed rootfs"
            sleep 1
            tar -xzpf "$ROOTFS_TARBALL" -C "$extract_dir"
            ;;
        *)
            echo "Unsupported rootfs archive (expected .tar.gz or .tgz): $ROOTFS_TARBALL" >&2
            exit 1
            ;;
    esac

    [ -x "$extract_dir/bin/bash" ] || {
        echo "Archive did not contain an executable /bin/bash" >&2
        exit 1
    }
    [ ! -d "$ROOTFS" ] || rmdir "$ROOTFS"
    mv "$extract_dir" "$ROOTFS"
    trap - EXIT HUP INT TERM
fi

[ -d "$PM_APP/PortMaster" ] || {
    echo "Extract trimui.portmaster.zip into $SDCARD/Apps first." >&2
    exit 1
}

export ROOTFS SDCARD
. "$SCRIPT_DIR/trimui-chroot-mounts.sh"
trimui_mount_chroot

architecture=$(chroot "$ROOTFS" /usr/bin/dpkg --print-architecture)
release=$(chroot "$ROOTFS" /bin/sh -c '. /etc/os-release; printf "%s:%s" "$ID" "$VERSION_ID"')
[ "$architecture" = arm64 ] || {
    echo "Expected arm64 rootfs, got: $architecture" >&2
    exit 1
}
case "$release" in
    ubuntu:24.04*) ;;
    *) echo "Expected Ubuntu 24.04, got: $release" >&2; exit 1 ;;
esac

ubuntu_sources="$ROOTFS/etc/apt/sources.list.d/ubuntu.sources"
if [ -f "$ubuntu_sources" ]; then
    sources_backup="$ubuntu_sources.before-china-mirror"
    [ -e "$sources_backup" ] || cp -p "$ubuntu_sources" "$sources_backup"
    sed -i \
        "s|^URIs: .*ubuntu-ports/\$|URIs: $UBUNTU_PORTS_MIRROR|" \
        "$ubuntu_sources"
    echo "Ubuntu ports mirror: $UBUNTU_PORTS_MIRROR"
fi

chroot "$ROOTFS" /bin/bash -lc '
    set -e
    export DEBIAN_FRONTEND=noninteractive LANG=C.UTF-8 LC_ALL=C.UTF-8
    cat > /etc/apt/apt.conf.d/99trimui-small-cache <<'"'"'EOF'"'"'
Acquire::Languages "none";
Dir::Cache::pkgcache "";
Dir::Cache::srcpkgcache "";
APT::Keep-Downloaded-Packages "false";
EOF
    apt-get update -o APT::Sandbox::User=root
    apt-get install -y -o APT::Sandbox::User=root \
        python3 \
        ca-certificates \
        libfreetype6 \
        libopenal1 \
        libmodplug1 \
        libvorbisfile3 \
        libtheora0 \
        libogg0 \
        libmpg123-0 \
        libmad0 \
        libusb-1.0-0 \
        usbutils \
        xz-utils

    # Runtime images do not need downloaded packages or repository indexes.
    # Only clean after every package has installed successfully.
    apt-get clean
    rm -f /var/cache/apt/pkgcache.bin /var/cache/apt/srcpkgcache.bin
    rm -rf /var/lib/apt/lists/*
    mkdir -p /var/cache/apt/archives/partial /var/lib/apt/lists/partial
'

cp "$SCRIPT_DIR/trimui-portmaster-chroot-inner.sh" \
    "$ROOTFS/usr/local/bin/trimui-portmaster-chroot"
cp "$SCRIPT_DIR/trimui-portmaster-chroot-launch.sh" \
    "$PM_APP/launch.chroot.sh"
cp "$SCRIPT_DIR/trimui-chroot-mounts.sh" \
    "$PM_APP/trimui-chroot-mounts.sh"
{
    printf "ROOTFS='%s'\n" "$ROOTFS"
    printf "SDCARD='%s'\n" "$SDCARD"
    printf "PM_APP='%s'\n" "$PM_APP"
} > "$PM_APP/trimui-chroot.conf"
chmod 0755 \
    "$ROOTFS/usr/local/bin/trimui-portmaster-chroot" \
    "$PM_APP/launch.chroot.sh" \
    "$PM_APP/trimui-chroot-mounts.sh"

config_file="$PM_APP/config.json"
chroot "$ROOTFS" /usr/bin/python3 - "$config_file" <<'PY'
import json
import os
import sys

path = sys.argv[1]
with open(path, encoding="utf-8") as fp:
    data = json.load(fp)
data["launch"] = "launch.chroot.sh"
temporary = path + ".tmp"
with open(temporary, "w", encoding="utf-8") as fp:
    json.dump(data, fp, ensure_ascii=False, indent=2)
    fp.write("\n")
os.replace(temporary, path)
PY

echo "Verifying DNS and host SDL..."
chroot "$ROOTFS" /usr/bin/getent hosts ports.ubuntu.com >/dev/null
chroot "$ROOTFS" /bin/bash -lc '
    export LD_LIBRARY_PATH=/usr/trimui/lib:/opt/trimui-host/compat-lib:/mnt/SDCARD/System/lib
    export PYSDL2_DLL_PATH=/usr/trimui/lib
    python3 -c '\''import ctypes; ctypes.CDLL("libSDL2-2.0.so.0"); print("SDL2 load: OK")'\''
'

echo "Ubuntu chroot ready: $ROOTFS"
echo "PortMaster entry: $PM_APP/launch.chroot.sh"
