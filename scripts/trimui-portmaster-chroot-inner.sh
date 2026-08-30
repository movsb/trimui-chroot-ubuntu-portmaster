#!/bin/bash
set -o pipefail

export HOME=/root
export USER=root
export LOGNAME=root
export LANG=C.UTF-8
export LC_ALL=C.UTF-8
export TERM=linux
PM_APP=${PM_APP:-/mnt/SDCARD/Apps/UbuntuPortMasterRuntime}

# Ordering is intentional: TrimUI's customized SDL comes first, its GPU/EGL
# userspace follows, and Ubuntu supplies libc and ordinary dependencies.
export LD_LIBRARY_PATH="/usr/trimui/lib:/opt/trimui-host/compat-lib:/mnt/SDCARD/System/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export PYSDL2_DLL_PATH=/usr/trimui/lib

mkdir -p /tmp/shm /run/user/0
chmod 1777 /tmp/shm
chmod 700 /run/user/0
export XDG_RUNTIME_DIR=/run/user/0

if [ "$#" -gt 0 ]; then
    exec "$@"
fi

exec /bin/bash "$PM_APP/launch.sh"
