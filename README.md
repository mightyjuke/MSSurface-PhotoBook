# Surface PhotoBook

A small, self-hosted digital photo frame built for the original 32 GB Microsoft Surface RT. It starts a full-screen slideshow when the desktop logs in and exposes a touch-friendly photo manager to other browsers on the same network.

The application is deliberately one ARMv7 binary with embedded HTML, CSS, and JavaScript. It has no database, Node.js runtime, container engine, or cloud dependency.

## What is included

- Full-screen, automatically refreshing slideshow
- Multiple-photo upload and deletion from a browser
- Shuffle, timing, fade/slide, contain/cover, background, and clock settings
- Photos and settings stored as ordinary files under `/var/lib/photobook`
- Optional password protection for all management routes
- OpenRC service and XFCE autostart entry
- Windows-to-Linux ARMv7 cross-build script
- Surface RT Wi-Fi power-saving workaround
- Checksum-verified automatic updates from GitHub with health-check rollback

## Important hardware check

This project targets the **first-generation Surface RT**, with an NVIDIA Tegra 3 processor, 2 GB RAM, and a 1366×768 display. It does not use the `linux-surface` kernel project intended for Intel/AMD Surface devices.

Do not flash anything until the exact model is confirmed. A Surface 2 also ran Windows RT but uses Tegra 4 and needs a different device port. In Windows RT, open **PC settings → PC and devices → PC info** and confirm the processor is NVIDIA Tegra 3.

Raspberry Pi OS is not a suitable image for this tablet. It is optimized and packaged for Raspberry Pi hardware; sharing the ARM instruction set is not enough to supply the Surface bootloader, kernel, device tree, and drivers. The supported starting point here is the Surface RT community port of **postmarketOS**.

## Recommended installation path

This is a two-stage job. First prove Linux and the hardware from USB. Only then replace the internal Windows installation.

### 1. Back up and unlock the Surface

1. Copy every file you want to keep off the tablet and create/retain Windows RT recovery media if possible.
2. Keep the charger connected throughout the bootloader work.
3. Follow the current [Open Surface RT preparation and boot instructions](https://open-rt.gitbook.io/open-surfacert). This includes test signing and `yahallo` to bypass Secure Boot on supported firmware.
4. Follow the [postmarketOS Surface RT device page](https://wiki.postmarketos.org/wiki/Microsoft_Surface_RT_(microsoft-surface-rt)). Use the current prebuilt Surface RT image or `pmbootstrap`, and select **xfce4** when asked for a user interface.
5. Boot and test from USB first. The Surface RT page currently documents USB boot—not SD boot—and lists display, touch, Wi-Fi, battery reporting, internal storage, and SD access as working. Bluetooth and the cameras are not currently working.

Unlocking and replacing the boot chain can make the tablet temporarily unbootable. Those steps intentionally remain in the maintained device documentation instead of being duplicated in an automated script here.

### 2. Move postmarketOS to internal storage

After the USB system is stable and Wi-Fi/touch/display have been tested, use the current eMMC procedure on the Surface RT device page. At the time this project was prepared, its documented flow was to install `pmbootstrap` in the USB-booted system and install to `/dev/mmcblk0`. Re-check the page before running it; device installation commands can change.

The 32 GB device has enough space for postmarketOS, Chromium, this app, and a moderate photo library. Keep the original high-resolution collection backed up elsewhere and upload display-sized copies where practical.

## Build the application on Windows

Install a current Go toolchain, then run from PowerShell:

```powershell
./scripts/build-armv7.ps1
```

This creates `dist/photobook-armv7`, a statically linked Linux ARMv7 executable. Copy the entire repository (or at least `dist/photobook-armv7` and the `deploy` directory) to a USB drive accessible by the Surface.

## Install PhotoBook on postmarketOS

From an XFCE terminal on the Surface:

```sh
cd /path/to/MSSurface-PhotoBook
sudo sh deploy/install-postmarketos.sh dist/photobook-armv7
```

The installer creates a random admin password and prints it once. To choose one beforehand:

```sh
sudo PHOTOBOOK_PASSWORD='choose-a-long-password' sh deploy/install-postmarketos.sh dist/photobook-armv7
```

The installer will:

- install Chromium, `xset`, and `iw` from Alpine/postmarketOS packages;
- install the app as an OpenRC service;
- add an XFCE autostart entry for Chromium kiosk mode;
- disable display blanking in the kiosk session;
- disable Wi-Fi power saving at boot, avoiding a documented Surface RT resume problem.

Reboot. After XFCE logs in, Chromium should open `http://127.0.0.1:8080/display/` full-screen. If your image presents a login screen, enable automatic login for the regular desktop user so that the XFCE autostart entry can run.

### Raspberry Pi OS alternative

The same ARMv7 binary also runs on 32-bit Raspberry Pi OS. On a Raspberry Pi OS desktop installation, use the systemd installer instead:

```sh
sudo PHOTOBOOK_PASSWORD='choose-a-long-password' sh deploy/install-raspios.sh dist/photobook-armv7
```

It installs the server as `photobook.service` and adds the same desktop kiosk autostart entry. Desktop automatic login must be enabled for the kiosk browser to open after boot.

## Automatic updates

Every push to `main` runs the test suite, builds the Linux ARMv7 binary, and publishes it with a SHA-256 checksum to the stable `edge` prerelease. Installed Raspberry Pi OS devices check that release approximately every 15 minutes; postmarketOS devices use the system periodic scheduler.

The updater:

1. Downloads the binary and checksum over HTTPS from the public GitHub release.
2. Refuses files whose checksum does not match.
3. Skips installation when the binary is already current.
4. Replaces the executable atomically and restarts PhotoBook.
5. Waits for the local health endpoint and restores the previous binary if startup fails.
6. Causes an open kiosk to reload itself when it observes a new build version.

On Raspberry Pi OS:

```sh
# Check the schedule
systemctl list-timers photobook-update.timer

# Check immediately
sudo systemctl start photobook-update.service

# Read update results
journalctl -u photobook-update.service

# Disable automatic updates
sudo systemctl disable --now photobook-update.timer
```

The release source can be changed in `/etc/default/photobook-updater`. No GitHub token is required while the configured repository and release remain public.

## Add photos from another device

Connect a phone or computer to the same Wi-Fi network. On the Surface, find its address with:

```sh
ip -4 address show mlan0
```

If its address is `192.168.1.42`, open:

```text
http://192.168.1.42:8080/admin/
```

Sign in with username `admin` and the password printed by the installer. The frame polls for changes every 15 seconds, so uploads, deletions, and setting changes appear without restarting it.

The manager uses HTTP on the local network. Do not expose port 8080 directly to the internet. Basic authentication prevents casual LAN access but does not encrypt traffic; use a trusted home network or place an HTTPS reverse proxy/VPN in front of it.

## Operations

```sh
# Service status
sudo rc-service photobook status

# Restart after changing /etc/conf.d/photobook
sudo rc-service photobook restart

# Live application log
tail -f /var/log/photobook.log

# Back up all photos and settings
sudo tar -czf photobook-backup.tgz /var/lib/photobook
```

To change the password, edit `PHOTOBOOK_ADMIN_PASSWORD` in `/etc/conf.d/photobook`, restart the service, and reload the admin page.

## Local development

```powershell
go test ./...
$env:PHOTOBOOK_ADMIN_PASSWORD = "dev-password"
go run .
```

Open `http://localhost:8080/admin/` for management and `http://localhost:8080/display/` for the frame.

Environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `PHOTOBOOK_ADDRESS` | `:8080` | HTTP listen address |
| `PHOTOBOOK_DATA_DIR` | `./data` | Photo and state storage |
| `PHOTOBOOK_ADMIN_PASSWORD` | empty | Password for username `admin`; empty disables authentication |

Supported uploads are JPEG, PNG, GIF, and WebP. A single request is limited to 512 MB. For this 2 GB tablet, photos around the display's native resolution—or up to roughly 2560 px on the long edge—will load faster and leave more storage free.
