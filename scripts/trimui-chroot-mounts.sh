#!/bin/sh

# Shared mount setup for the TrimUI Ubuntu chroot.
# The caller may override ROOTFS and SDCARD before sourcing this file.
ROOTFS=${ROOTFS:-/mnt/UDISK/ubuntu-portmaster-rootfs}
SDCARD=${SDCARD:-/mnt/SDCARD}
PM_APP=${PM_APP:-$SDCARD/Apps/UbuntuPortMasterRuntime}

trimui_is_mounted() {
    grep -qs " $1 " /proc/mounts
}

trimui_mount_dir() {
    source_path=$1
    target_path=$2
    mkdir -p "$target_path"
    trimui_is_mounted "$target_path" || mount --bind "$source_path" "$target_path"
}

trimui_mount_file() {
    source_path=$1
    target_path=$2
    [ -e "$source_path" ] || return 0
    mkdir -p "$(dirname "$target_path")"
    [ -e "$target_path" ] || : > "$target_path"
    trimui_is_mounted "$target_path" || mount --bind "$source_path" "$target_path"
}

trimui_make_compat_libs() {
    compat_lib="$ROOTFS/opt/trimui-host/compat-lib"
    mkdir -p "$compat_lib"

    # /usr/lib is mounted for access, but is deliberately not placed directly
    # in LD_LIBRARY_PATH. Old host libz/liblzma/ALSA would break Ubuntu tools.
    for library in \
        libEGL.so \
        libGLESv2.so \
        libGLES_CM.so \
        libIMGegl.so \
        libpvrNULL_WSEGL.so \
        libsrv_um.so \
        libglslcompiler.so \
        libusc.so \
        libpng12.so.0.56.0 \
        libjpeg.so.9.1.0
    do
        [ -e "$ROOTFS/opt/trimui-host/usr/lib/$library" ] || continue
        ln -sf "../usr/lib/$library" "$compat_lib/$library"
    done

    ln -sf libEGL.so "$compat_lib/libEGL.so.1"
    ln -sf libGLESv2.so "$compat_lib/libGLESv2.so.2"
    ln -sf libGLES_CM.so "$compat_lib/libGLES_CM.so.1"
    ln -sf libpng12.so.0.56.0 "$compat_lib/libpng12.so.0"
    ln -sf libjpeg.so.9.1.0 "$compat_lib/libjpeg.so.9"
}

trimui_mount_chroot() {
    [ -x "$ROOTFS/bin/bash" ] || {
        echo "Not an Ubuntu rootfs: $ROOTFS" >&2
        return 1
    }

    mkdir -p \
        "$ROOTFS/proc" \
        "$ROOTFS/sys" \
        "$ROOTFS/dev" \
        "$ROOTFS/tmp" \
        "$ROOTFS/mnt/SDCARD"

    trimui_is_mounted "$ROOTFS/proc" || mount -t proc proc "$ROOTFS/proc" || return 1
    trimui_is_mounted "$ROOTFS/sys" || mount -t sysfs sysfs "$ROOTFS/sys" || return 1
    trimui_mount_dir /dev "$ROOTFS/dev" || return 1
    # /dev from the rootfs is hidden by the bind mount above. Create/check the
    # devpts mountpoint in the now-visible host /dev tree.
    mkdir -p "$ROOTFS/dev/pts"
    trimui_is_mounted "$ROOTFS/dev/pts" || mount -t devpts devpts "$ROOTFS/dev/pts" || return 1
    trimui_is_mounted "$ROOTFS/tmp" || mount -t tmpfs tmpfs "$ROOTFS/tmp" || return 1
    # Mounting tmpfs hides everything previously created under rootfs/tmp.
    # Runtime directories therefore must be created after the mount.
    mkdir -p "$ROOTFS/tmp/shm"
    chmod 1777 "$ROOTFS/tmp" "$ROOTFS/tmp/shm"

    trimui_mount_dir "$SDCARD" "$ROOTFS/mnt/SDCARD" || return 1
    mkdir -p "$SDCARD/Data/ports"
    # Games live on the SD card, not inside the disposable rootfs. Mount the
    # parent first, then overlay PortMaster's program directory as its child.
    trimui_mount_dir "$SDCARD/Data/ports" "$ROOTFS/roms/ports" || return 1
    trimui_mount_dir "$PM_APP/PortMaster" "$ROOTFS/roms/ports/PortMaster" || return 1
    trimui_mount_file /etc/openwrt_release "$ROOTFS/etc/openwrt_release" || return 1
    trimui_mount_file /etc/version "$ROOTFS/etc/version" || return 1

    # TrimUI's customized SDL is mounted at its native path. Generic host GPU
    # libraries are namespaced so Ubuntu's own /usr/lib remains visible.
    trimui_mount_dir /usr/trimui/lib "$ROOTFS/usr/trimui/lib" || return 1
    trimui_mount_dir /usr/lib "$ROOTFS/opt/trimui-host/usr/lib" || return 1
    trimui_make_compat_libs

    # The host resolver can change whenever Wi-Fi reconnects.
    cp -L /etc/resolv.conf "$ROOTFS/etc/resolv.conf" 2>/dev/null || true
}
