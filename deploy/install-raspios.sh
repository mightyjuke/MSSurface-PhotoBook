#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "Run as root: sudo ./install-raspios.sh /path/to/photobook-armv7" >&2
    exit 1
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
binary_path=${1:-"$script_dir/../dist/photobook-armv7"}

if [ ! -f "$binary_path" ]; then
    echo "PhotoBook ARMv7 binary not found: $binary_path" >&2
    exit 1
fi

case "${PHOTOBOOK_PASSWORD:-}" in
    *[!A-Za-z0-9_.!@#%+-]*)
        echo "PHOTOBOOK_PASSWORD may contain letters, numbers, and ._!@#%+- only." >&2
        exit 1
        ;;
esac

if [ -z "${PHOTOBOOK_PASSWORD:-}" ]; then
    PHOTOBOOK_PASSWORD=$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')
fi

if ! command -v chromium >/dev/null 2>&1 && ! command -v chromium-browser >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y chromium-browser
fi
if ! command -v curl >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates
fi

if ! id photobook >/dev/null 2>&1; then
    useradd --system --home-dir /var/lib/photobook --shell /usr/sbin/nologin photobook
fi

install -d -o photobook -g photobook -m 0750 /var/lib/photobook
install -m 0755 "$binary_path" /usr/local/bin/photobook
install -m 0755 "$script_dir/photobook-kiosk" /usr/local/bin/photobook-kiosk
install -m 0644 "$script_dir/photobook.service" /etc/systemd/system/photobook.service
install -m 0644 "$script_dir/photobook-kiosk.desktop" /etc/xdg/autostart/photobook-kiosk.desktop
install -m 0755 "$script_dir/photobook-update" /usr/local/sbin/photobook-update
install -m 0644 "$script_dir/photobook-update.service" /etc/systemd/system/photobook-update.service
install -m 0644 "$script_dir/photobook-update.timer" /etc/systemd/system/photobook-update.timer

cat > /etc/default/photobook <<EOF
PHOTOBOOK_ADDRESS="0.0.0.0:8080"
PHOTOBOOK_DATA_DIR="/var/lib/photobook"
PHOTOBOOK_ADMIN_PASSWORD="$PHOTOBOOK_PASSWORD"
EOF
chmod 0640 /etc/default/photobook
cat > /etc/default/photobook-updater <<'EOF'
PHOTOBOOK_UPDATE_BASE_URL="https://github.com/mightyjuke/MSSurface-PhotoBook/releases/download/edge"
EOF
chmod 0644 /etc/default/photobook-updater

systemctl daemon-reload
systemctl enable --now photobook.service
systemctl enable --now photobook-update.timer

echo "PHOTOBOOK_ADMIN_PASSWORD=$PHOTOBOOK_PASSWORD"
