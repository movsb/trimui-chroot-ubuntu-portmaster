# TrimUI Ubuntu 24.04 chroot + PortMaster 可复现构建

这套代码从一个 Ubuntu 24.04 arm64 rootfs 开始，在 TrimUI TinaLinux 宿主上构建可运行 PortMaster 和 aarch64 port 游戏的 chroot。宿主定制库只通过 bind mount 使用，不复制进 Ubuntu。

## 目录约定

```text
/mnt/UDISK/ubuntu-portmaster-rootfs                         Ubuntu rootfs
/mnt/SDCARD/Apps/UbuntuPortMasterRuntime                    PortMaster App 目录
/mnt/SDCARD/Apps/UbuntuPortMasterRuntime/PortMaster         PortMaster 程序本体
/mnt/SDCARD/Data/ports                    PortMaster 游戏和启动脚本
```

构建脚本默认使用上述路径，也可把 rootfs 路径作为第一个参数传入。

## 前置输入

1. Ubuntu 24.04 arm64 base rootfs 压缩包。
2. 已将 `trimui.portmaster.zip` 解压到 `/mnt/SDCARD/Apps`。
3. 将本目录配套的四个脚本放在 TrimUI 上的同一个目录：

```text
trimui-ubuntu-chroot-setup.sh
trimui-chroot-mounts.sh
trimui-portmaster-chroot-launch.sh
trimui-portmaster-chroot-inner.sh
trimui-ubuntu-chroot-unmount.sh
```

rootfs 必须位于 ext4、f2fs 或其它支持 Unix 权限、符号链接和设备节点的文件系统。不能直接解压到 FAT32；FAT32 不具备完整的 Linux rootfs 语义。

## 从压缩包构建

假定 rootfs 压缩包为：

```text
/mnt/SDCARD/ubuntu-base-24.04.4-base-arm64.tar.gz
```

执行：

```sh
cd /放置脚本的目录
chmod +x trimui-*.sh
./trimui-ubuntu-chroot-setup.sh \
    /mnt/UDISK/ubuntu-portmaster-rootfs \
    /mnt/SDCARD/ubuntu-base-24.04.4-base-arm64.tar.gz
```

如果配置的 rootfs 已经有效，第二个参数可以省略：

```sh
./trimui-ubuntu-chroot-setup.sh /mnt/UDISK/ubuntu-portmaster-rootfs
```

脚本是幂等的：重复运行会刷新挂载、DNS、Ubuntu 软件包和启动器，不会清空已有 rootfs。`.tar.gz` 先经 `gzip -t` 校验，再由 BusyBox `tar -xzpf` 解压到临时目录，验证 `bin/bash` 后原子改名。若目标目录非空但不是有效 rootfs，脚本拒绝解压，避免覆盖未知数据。

安装软件包前，脚本会把 deb822 格式的 `ubuntu.sources` 备份为 `ubuntu.sources.before-china-mirror`，并将 arm64 的 `ports.ubuntu.com/ubuntu-ports` 替换为阿里云 Ubuntu Ports 镜像。可通过 `UBUNTU_PORTS_MIRROR` 环境变量覆盖默认地址。

## 初始化脚本实际做了什么

### 1. 验证 rootfs

脚本检查：

```text
dpkg architecture = arm64
os-release         = ubuntu:24.04
```

旧 Ubuntu 18.04 的 glibc 2.27 无法加载当前 TrimUI SDL2 所需的 `GLIBC_2.29`；Ubuntu 24.04 的 glibc 2.39 已验证兼容。

### 2. 建立 chroot 挂载

```text
宿主 /proc                       -> rootfs/proc
宿主 /sys                        -> rootfs/sys
宿主 /dev                        -> rootfs/dev
devpts                           -> rootfs/dev/pts
独立 tmpfs                       -> rootfs/tmp
宿主 /mnt/SDCARD                 -> rootfs/mnt/SDCARD
SD 游戏目录 Data/ports           -> rootfs/roms/ports
SD PortMaster 程序目录            -> rootfs/roms/ports/PortMaster
宿主 /usr/trimui/lib             -> rootfs/usr/trimui/lib
宿主 /usr/lib                    -> rootfs/opt/trimui-host/usr/lib
宿主 /etc/openwrt_release        -> rootfs/etc/openwrt_release
宿主 /etc/version                -> rootfs/etc/version
```

`/roms/ports` 整体映射到 SD 卡的 `Data/ports`，使游戏与可丢弃的 rootfs 分离。随后 `/roms/ports/PortMaster` 再映射到 App 程序目录；部分 port 脚本硬编码在该位置寻找 `control.txt`，因此这个子挂载仍需保留。

`/dev/shm` 在 Ubuntu 中指向 `/tmp/shm`，初始化会创建并设置 `1777` 权限。

### 3. 配置 DNS

每次挂载或启动都会执行：

```sh
cp -L /etc/resolv.conf "$ROOTFS/etc/resolv.conf"
```

原因是 Wi-Fi 重连可能替换宿主的 resolver 文件，chroot 不运行独立的 systemd-resolved。

### 4. 更新 APT 并安装依赖

脚本使用：

```sh
apt-get update -o APT::Sandbox::User=root
apt-get install -y -o APT::Sandbox::User=root ...
```

TrimUI 内核启用了 Android paranoid networking。APT 默认降权到 `_apt` 用户后会失去联网权限，表现为 `_apt` 无法解析域名，而 root 的 `getent` 正常。显式使用 root sandbox user 是该设备内核下的必要兼容参数。

安装的软件包：

```text
python3
ca-certificates
libfreetype6
libopenal1
libmodplug1
libvorbisfile3
libtheora0
libogg0
libmpg123-0
libmad0
libusb-1.0-0
usbutils
xz-utils
```

Python 3 是 PortMaster `pugwash` 的直接依赖；其 shebang 为 `/usr/bin/env python3`。

### 5. 使用宿主 SDL、EGL 和 PowerVR 库

TrimUI 的 SDL2 含设备专用视频后端，不能用 Ubuntu 通用 SDL2 替代。它从以下挂载读取：

```text
/usr/trimui/lib
```

宿主 `/usr/lib` 虽然挂载到 chroot，但不能整体加入 `LD_LIBRARY_PATH`。整体加入会让 Ubuntu 的 Python、xz 和 ALSA 错误选择宿主旧版 `libz`、`liblzma`、`libasound`。

挂载模块因此创建一个只含符号链接的筛选视图：

```text
/opt/trimui-host/compat-lib
```

它只暴露：

```text
libEGL.so
libGLESv2.so
libGLES_CM.so
libIMGegl.so
libpvrNULL_WSEGL.so
libsrv_um.so
libglslcompiler.so
libusc.so
libpng12.so.0
libjpeg.so.9
```

这些都是指向 bind-mounted 宿主目录的符号链接，不是库文件副本。Ubuntu 自己继续提供 libc、libm、libstdc++、zlib、xz 和 ALSA。

运行时搜索顺序为：

```text
/usr/trimui/lib
/opt/trimui-host/compat-lib
/mnt/SDCARD/System/lib
Ubuntu 系统默认库目录
```

### 6. 安装两层启动器

宿主入口：

```text
/mnt/SDCARD/Apps/UbuntuPortMasterRuntime/launch.chroot.sh
```

chroot 内入口：

```text
/usr/local/bin/trimui-portmaster-chroot
```

宿主入口负责恢复所有挂载并进入 chroot；内层入口设置 UTF-8、`LD_LIBRARY_PATH`、`PYSDL2_DLL_PATH` 和 `XDG_RUNTIME_DIR`。

无参数时启动 PortMaster：

```sh
/mnt/SDCARD/Apps/UbuntuPortMasterRuntime/launch.chroot.sh
```

带参数时可启动某个 port：

```sh
/mnt/SDCARD/Apps/UbuntuPortMasterRuntime/launch.chroot.sh \
    /bin/bash '/mnt/SDCARD/Data/ports/游戏.sh'
```

### 7. 修改 TrimUI App 入口

初始化脚本把：

```text
/mnt/SDCARD/Apps/UbuntuPortMasterRuntime/config.json
```

中的 `launch` 改为：

```json
"launch": "launch.chroot.sh"
```

`config.json` 由本 App 直接管理，不创建备份。官方 `launch.sh` 不修改、也不备份；它由 chroot 内层入口调用。TrimUI 菜单始终进入本 App 管理的 `launch.chroot.sh`。

PortMaster 生成的原生游戏入口则先调用 `trimui-chroot-manager --launch-port <PORTSCRIPT>`。manager 根据对应 `port.json` 的 `attr.rtr` 判断该 port 是否自带本体；`rtr: false` 时使用 fbiw AlertDialog 显示 `attr.inst` 或 `attr.inst_md` 安装说明，确认后才通过 `launch.chroot.sh` 进入 chroot 启动游戏。

## 手工进入 chroot

先运行挂载模块，再进入：

```sh
ROOTFS=/mnt/UDISK/ubuntu-portmaster-rootfs
SDCARD=/mnt/SDCARD
. ./trimui-chroot-mounts.sh
trimui_mount_chroot
chroot "$ROOTFS" /bin/bash
```

进入后建议环境：

```sh
export LANG=C.UTF-8
export LC_ALL=C.UTF-8
export LD_LIBRARY_PATH=/usr/trimui/lib:/opt/trimui-host/compat-lib:/mnt/SDCARD/System/lib
```

SSH 终端显示和输入中文只需要 UTF-8 locale；终端字体由 SSH 客户端负责，chroot 不需要安装中文字体。PortMaster 图形界面的中文字体则来自它自带的 NotoSansSC。

## 验证

DNS：

```sh
chroot /mnt/UDISK/ubuntu-portmaster-rootfs getent hosts ports.ubuntu.com
```

SDL2：

```sh
chroot /mnt/UDISK/ubuntu-portmaster-rootfs /bin/bash -lc '
export LD_LIBRARY_PATH=/usr/trimui/lib:/opt/trimui-host/compat-lib:/mnt/SDCARD/System/lib
python3 -c '\''import ctypes; ctypes.CDLL("libSDL2-2.0.so.0"); print("OK")'\''
'
```

PortMaster 日志：

```text
/mnt/SDCARD/Apps/UbuntuPortMasterRuntime/PortMaster/log.txt
```

游戏日志通常在：

```text
/mnt/SDCARD/Data/ports/<游戏ID>/log.txt
```

## 卸载挂载

先退出 PortMaster 和游戏，然后执行：

```sh
./trimui-ubuntu-chroot-unmount.sh /mnt/UDISK/ubuntu-portmaster-rootfs
```

脚本按依赖反序卸载。仍被进程占用的挂载不会强制拆除，只报告 `Still busy`。

## 不做的事情

- 不复制宿主 SDL/EGL/GPU 库进 Ubuntu。
- 不复制或替换 Ubuntu 的 libc、libm、libstdc++。
- 不把宿主整个 `/usr/lib` 放入全局动态库搜索路径。
- 不依赖 systemd；chroot 不是完整启动的虚拟机。
- 不把 rootfs 放在 FAT32。
