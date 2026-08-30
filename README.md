# Ubuntu PortMaster 环境管理器

面向 TrimUI 的 fbiw App，用于全量重装和卸载 Ubuntu 24.04 arm64 chroot + PortMaster 环境。

## 输入文件

点击“清理并全量安装”时，App 总是先卸载挂载并删除现有 Ubuntu rootfs 与 PortMaster 程序，再执行完整安装，不再区分首次安装和修复。游戏与存档保留。如果安装包不存在，App 会列出手工放置路径和下载地址：按 A 清理并自动下载，按 B 取消。服务器返回文件总大小时，界面显示百分比和“已下载 / 总大小”；未返回总大小时显示已下载大小。

内置下载地址：

```text
Ubuntu: https://cdimage.ubuntu.com/ubuntu-base/releases/24.04.4/release/ubuntu-base-24.04.4-base-arm64.tar.gz
PortMaster: https://github.com/PortsMaster/PortMaster-GUI/releases/download/2026.07.28-1212/trimui.portmaster.zip
```

自动下载的落盘位置：

```text
/mnt/SDCARD/ubuntu-base-24.04.4-base-arm64.tar.gz
/mnt/SDCARD/trimui.portmaster.zip
```

下载先写入同目录的 `.part` 文件，成功后原子改名；失败不会把残缺文件当成安装包。

TrimUI 宿主固件可能没有系统 CA 根证书。App 在二进制中内嵌 `assets/cacert.pem`（curl CA Extract 发布的 Mozilla CA bundle），并将其追加到 Go 的系统证书池，因此首次下载 Ubuntu rootfs 时不依赖宿主或 chroot 提供证书。TLS 证书校验保持开启，最低版本为 TLS 1.2。

手工下载时，Ubuntu rootfs 包必须放在：

```text
/mnt/SDCARD/ubuntu-base-24.04.4-base-arm64.tar.gz
```

PortMaster 包必须放在：

```text
/mnt/SDCARD/trimui.portmaster.zip
```

安装器只扫描 `/mnt/SDCARD` 根目录，不扫描 App 目录或 `payload` 子目录。

PortMaster 官方 ZIP 虽然自带顶层 `Apps/PortMaster`，安装器会先解压到临时目录，再移动到本项目的命名空间。默认程序目录为 `/mnt/SDCARD/Apps/UbuntuPortMasterRuntime`，Ubuntu 安装到 `/mnt/UDISK/ubuntu-portmaster-rootfs`，不会占用通用的 `PortMaster` 或 `ubuntu` 目录。

需要更换命名空间时，只修改 `manager.go` 顶部的 `rootFSDirName` 和 `portMasterAppDirName` 两个常量；Go 管理器会把最终路径传给初始化脚本，启动器再从生成的 `trimui-chroot.conf` 读取路径。

PortMaster 官方 `launch.sh` 和 `PortMaster/control.txt` 都带有固定的 `/mnt/SDCARD/Apps/PortMaster/PortMaster` controlfolder，而且自更新可能恢复这些文件。因此只由本项目的宿主入口在每次启动、进入 chroot 之前改写并校验路径，同时为 `cd` 添加失败即退出保护。setup 不重复维护这套逻辑。

入口强制使用 PortMaster 的 TrimUI `ports` 模式，在 `/mnt/SDCARD/Ports/portmaster-游戏名` 创建原生 App 目录。每个入口包含独立的 `launch.sh`；TrimUI 只启动该脚本，再由它通过本项目的 `launch.chroot.sh` 执行 chroot 内的 `/roms/ports/*.sh`。启动自愈会修复 PortMaster 更新或新装游戏产生的旧式入口。

初始化脚本默认将 arm64 APT 源从 Ubuntu 官方 ports 站切换为阿里云 `http://mirrors.aliyun.com/ubuntu-ports/`，原始 deb822 配置只备份一次为 `ubuntu.sources.before-china-mirror`。需要使用其它镜像时，可在调用脚本前设置 `UBUNTU_PORTS_MIRROR`。

APT 使用精简缓存配置：不下载翻译索引，不生成 `pkgcache.bin` 和 `srcpkgcache.bin`，也不保留下载后的 `.deb`。所有依赖成功安装后，脚本执行 `apt-get clean` 并清空 `/var/lib/apt/lists`；下次需要使用 APT 时先重新执行 `apt-get update`。

Ubuntu Base 下载物是 gzip 压缩的 tar archive。设备端初始化脚本先执行 `gzip -t`，再用 BusyBox `tar -xzpf` 解压到同一文件系统的临时目录；仅在确认 `bin/bash` 可执行后才将目录改名为配置的 rootfs 路径。解压失败或被中断时会清理临时目录。

## 构建

项目依赖同级工作区中的 fbiw：

```sh
GOEXPERIMENT=simd go test ./...
./make.bash
```

生成：

```text
UbuntuPortMaster.zip
```

解压到 `/mnt/SDCARD/Apps` 后可在 TrimUI Apps 页面运行。

## 卸载语义

卸载会永久删除环境，无法恢复。App 会：

1. 卸载 chroot 的全部挂载；
2. 若仍有 busy mount 则停止；
3. 删除配置的 rootfs（默认 `/mnt/UDISK/ubuntu-portmaster-rootfs`）；
4. 删除配置的 PortMaster App（默认 `/mnt/SDCARD/Apps/UbuntuPortMasterRuntime`）；
5. 删除自动下载的 Ubuntu 和 PortMaster 安装包。

`/mnt/SDCARD/Data/ports` 中的游戏与存档保留。

详细底层说明见 `docs/trimui-ubuntu-portmaster-reproducible.md`。
