#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "Run this installer as root: sudo ./install-postmarketos.sh /path/to/photobook-armv7" >&2
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

echo "Installing Chromium and X11 utilities..."
apk add chromium xset iw

if ! id photobook >/dev/null 2>&1; then
    adduser -S -D -H -h /var/lib/photobook photobook
fi

install -m 0755 "$binary_path" /usr/local/bin/photobook
install -m 0755 "$script_dir/photobook-kiosk" /usr/local/bin/photobook-kiosk
install -m 0755 "$script_dir/photobook.initd" /etc/init.d/photobook
install -m 0644 "$script_dir/photobook-kiosk.desktop" /etc/xdg/autostart/photobook-kiosk.desktop
install -d -o photobook -g photobook -m 0750 /var/lib/photobook

# Surface RT Wi-Fi can disappear after power-saving events. The device's
# postmarketOS notes recommend disabling Wi-Fi power management at every boot.
install -d -m 0755 /etc/local.d
cat > /etc/local.d/surface-wifi.start <<'EOF'
#!/bin/sh
/sbin/iw dev mlan0 set power_save off 2>/dev/null || true
EOF
chmod 0755 /etc/local.d/surface-wifi.start

cat > /etc/conf.d/photobook <<EOF
export PHOTOBOOK_ADDRESS="0.0.0.0:8080"
export PHOTOBOOK_DATA_DIR="/var/lib/photobook"
export PHOTOBOOK_ADMIN_PASSWORD="$PHOTOBOOK_PASSWORD"
EOF
chmod 0640 /etc/conf.d/photobook

rc-update add photobook default
rc-update add local default
rc-service photobook restart

echo
echo "PhotoBook is installed and will start at boot."
echo "Admin username: admin"
echo "Admin password: $PHOTOBOOK_PASSWORD"
echo
echo "Save that password now. Reboot once to verify kiosk startup."
