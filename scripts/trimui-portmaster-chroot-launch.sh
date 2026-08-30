#!/bin/sh

# Run the SD-card PortMaster package or a port command inside Ubuntu.
INNER_LAUNCHER=/usr/local/bin/trimui-portmaster-chroot

APP_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$APP_DIR/trimui-chroot.conf" || exit 1

# PortMaster self-updates can restore upstream's hardcoded Apps/PortMaster
# paths. Repair them before every launch so a completed update cannot break
# the next invocation of this namespaced installation.
CONTROLFOLDER="$PM_APP/PortMaster"
PLATFORM_FILE="$CONTROLFOLDER/pylibs/harbourmaster/platform.py"
PYLIB_ZIP="$CONTROLFOLDER/pylibs.zip"
PORT_LAUNCH="$MANAGER_EXECUTABLE --launch-port {{PORTSCRIPT}}"

. "$APP_DIR/trimui-chroot-mounts.sh" || exit 1
trimui_mount_chroot || exit 1

sed -i \
    "s|^controlfolder=.*|controlfolder=\"$CONTROLFOLDER\"|" \
    "$PM_APP/launch.sh" || exit 1
sed -i \
    "s|^export controlfolder=.*|export controlfolder=\"$CONTROLFOLDER\"|" \
    "$CONTROLFOLDER/control.txt" || exit 1
sed -i \
    's#^cd "$controlfolder"$#cd "$controlfolder" || exit 1#' \
    "$PM_APP/launch.sh" || exit 1

# On a fresh install PortMaster keeps platform.py in pylibs.zip. Patch the ZIP
# member atomically and let pugwash perform its normal extraction and cleanup.
# After first launch, patch the extracted file instead.
chroot "$ROOTFS" /usr/bin/python3 - "$PLATFORM_FILE" "$PYLIB_ZIP" "$PORT_LAUNCH" <<'PY' || exit 1
import os
import re
import sys
import zipfile

platform_file, pylib_zip, port_launch = sys.argv[1:]
member = "pylibs/harbourmaster/platform.py"

def patch(data):
    text = data.decode("utf-8")
    old_launch = '"launch":"{{PORTSCRIPT}}"'
    new_launch = f'"launch":"{port_launch}"'
    if old_launch in text:
        text = text.replace(old_launch, new_launch)
    elif new_launch not in text:
        pattern = r'"launch":"[^"\n]*\{\{PORTSCRIPT\}\}"'
        text, replacements = re.subn(pattern, new_launch, text, count=1)
        if replacements != 1:
            raise RuntimeError("unknown PortMaster launch template")

    old_icon = '("icon-pre" + image_file.suffix)'
    new_icon = '("icon.png")'
    if old_icon in text:
        text = text.replace(old_icon, new_icon)
    elif new_icon not in text:
        raise RuntimeError("unknown PortMaster icon template")
    return text.encode("utf-8")

if os.path.isfile(platform_file):
    with open(platform_file, "rb") as fp:
        data = patch(fp.read())
    temporary = platform_file + ".tmp"
    with open(temporary, "wb") as fp:
        fp.write(data)
    os.replace(temporary, platform_file)
elif os.path.isfile(pylib_zip):
    temporary = pylib_zip + ".tmp"
    with zipfile.ZipFile(pylib_zip, "r") as source, zipfile.ZipFile(temporary, "w") as target:
        found = False
        for info in source.infolist():
            data = source.read(info.filename)
            if info.filename == member:
                data = patch(data)
                found = True
            target.writestr(info, data)
    if not found:
        os.remove(temporary)
        raise RuntimeError(f"missing {member} in pylibs.zip")
    os.replace(temporary, pylib_zip)
else:
    raise RuntimeError("missing PortMaster platform.py and pylibs.zip")
PY

if ! grep -Fqx "controlfolder=\"$CONTROLFOLDER\"" "$PM_APP/launch.sh" ||
   ! grep -Fqx "export controlfolder=\"$CONTROLFOLDER\"" "$CONTROLFOLDER/control.txt" ||
   ! grep -Fqx 'cd "$controlfolder" || exit 1' "$PM_APP/launch.sh"
then
    echo "Could not repair PortMaster paths: $CONTROLFOLDER" >&2
    exit 1
fi

# Use TrimUI's native Apps-style entries under /mnt/SDCARD/Ports. PortMaster
# may rewrite its config during updates, so enforce this before every launch.
# TrimUI's launcher field accepts a script path, not a command plus arguments;
# turn PortMaster's command into a per-game launch.sh wrapper.
reconcile_native_ports() {
chroot "$ROOTFS" /usr/bin/python3 - "$CONTROLFOLDER/config/config.json" "$MANAGER_EXECUTABLE" <<'PY'
import json
import os
import shlex
import sys
from pathlib import Path

path = sys.argv[1]
manager = sys.argv[2]
os.makedirs(os.path.dirname(path), exist_ok=True)
if os.path.isfile(path):
    with open(path, encoding="utf-8") as fp:
        data = json.load(fp)
else:
    data = {}
data["trimui-port-mode"] = "ports"
temporary = path + ".tmp"
with open(temporary, "w", encoding="utf-8") as fp:
    json.dump(data, fp, ensure_ascii=False, indent=4)
    fp.write("\n")
os.replace(temporary, path)

prefix = f"{manager} --launch-port "
for port_dir in Path("/mnt/SDCARD/Ports").glob("portmaster-*"):
    config = port_dir / "config.json"
    if not config.is_file():
        continue
    with config.open(encoding="utf-8") as fp:
        entry = json.load(fp)
    command = entry.get("launch", "")
    if not command.startswith(prefix):
        continue
    arguments = shlex.split(command)
    launcher = port_dir / "launch.sh"
    launcher.write_text(
        "#!/bin/sh\nexec " + " ".join(shlex.quote(arg) for arg in arguments) + "\n",
        encoding="utf-8",
    )
    launcher.chmod(0o755)
    entry["launch"] = "launch.sh"
    temporary = str(config) + ".tmp"
    with open(temporary, "w", encoding="utf-8") as fp:
        json.dump(entry, fp, ensure_ascii=False, indent=4)
        fp.write("\n")
    os.replace(temporary, config)
PY
}

reconcile_native_ports || exit 1

normalize_native_icons() {
    [ -x "${MANAGER_EXECUTABLE:-}" ] || return 0
    for icon in /mnt/SDCARD/Ports/portmaster-*/icon.png
    do
        [ -f "$icon" ] || continue
        "$MANAGER_EXECUTABLE" --normalize-icon "$icon" || return 1
    done
}

normalize_native_icons || exit 1

chroot "$ROOTFS" /usr/bin/env PM_APP="$PM_APP" "$INNER_LAUNCHER" "$@"
status=$?

# A PortMaster installation creates entries while its UI is running. Convert
# those newly-created entries as soon as PortMaster exits.
reconcile_native_ports || exit 1
normalize_native_icons || exit 1

exit "$status"
