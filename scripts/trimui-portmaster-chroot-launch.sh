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
PORT_LAUNCH="$PM_APP/launch.chroot.sh /bin/bash {{PORTSCRIPT}}"
sed -i \
    "s|^controlfolder=.*|controlfolder=\"$CONTROLFOLDER\"|" \
    "$PM_APP/launch.sh" || exit 1
sed -i \
    "s|^export controlfolder=.*|export controlfolder=\"$CONTROLFOLDER\"|" \
    "$CONTROLFOLDER/control.txt" || exit 1
sed -i \
    's#^cd "$controlfolder"$#cd "$controlfolder" || exit 1#' \
    "$PM_APP/launch.sh" || exit 1
sed -i \
    "s#\"launch\":\"{{PORTSCRIPT}}\"#\"launch\":\"$PORT_LAUNCH\"#" \
    "$PLATFORM_FILE" || exit 1
# In ports mode upstream writes "icon.png" to config.json, but copies the
# artwork as icon-pre.png/icon-pre.jpg. Keep the configured filename stable;
# TrimUI detects the image format from its contents.
sed -i \
    's/("icon-pre" + image_file.suffix)/("icon.png")/' \
    "$PLATFORM_FILE" || exit 1
if ! grep -Fqx "controlfolder=\"$CONTROLFOLDER\"" "$PM_APP/launch.sh" ||
   ! grep -Fqx "export controlfolder=\"$CONTROLFOLDER\"" "$CONTROLFOLDER/control.txt" ||
   ! grep -Fqx 'cd "$controlfolder" || exit 1' "$PM_APP/launch.sh" ||
   ! grep -Fq "\"launch\":\"$PORT_LAUNCH\"" "$PLATFORM_FILE" ||
   ! grep -Fq 'target_file = new_port_dir / ("icon.png")' "$PLATFORM_FILE"
then
    echo "Could not repair PortMaster paths: $CONTROLFOLDER" >&2
    exit 1
fi

. "$APP_DIR/trimui-chroot-mounts.sh" || exit 1
trimui_mount_chroot || exit 1

# Use TrimUI's native Apps-style entries under /mnt/SDCARD/Ports. PortMaster
# may rewrite its config during updates, so enforce this before every launch.
# TrimUI's launcher field accepts a script path, not a command plus arguments;
# turn PortMaster's command into a per-game launch.sh wrapper.
reconcile_native_ports() {
chroot "$ROOTFS" /usr/bin/python3 - "$CONTROLFOLDER/config/config.json" "$PM_APP" <<'PY'
import json
import os
import shlex
import sys
from pathlib import Path

path = sys.argv[1]
pm_app = sys.argv[2]
with open(path, encoding="utf-8") as fp:
    data = json.load(fp)
data["trimui-port-mode"] = "ports"
temporary = path + ".tmp"
with open(temporary, "w", encoding="utf-8") as fp:
    json.dump(data, fp, ensure_ascii=False, indent=4)
    fp.write("\n")
os.replace(temporary, path)

prefix = f"{pm_app}/launch.chroot.sh /bin/bash "
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
    [ -x "${ICON_NORMALIZER:-}" ] || return 0
    for icon in /mnt/SDCARD/Ports/portmaster-*/icon.png
    do
        [ -f "$icon" ] || continue
        "$ICON_NORMALIZER" --normalize-icon "$icon" || return 1
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
