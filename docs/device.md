# Kobo installation and hardware validation

This is a native Go ARMv7 application. FBInk writes PNG snapshots to the actual e-ink framebuffer; the phone/computer editor is served by the Kobo itself. There is no remote rendering service. **No Kobo was attached during development.** Desktop tests and ARM builds do not establish touch calibration, retained sleep images, reliable RTC wakeups, Wi-Fi recovery, or battery endurance. The one-week battery goal is unmeasured.

## Build and dependencies

From the repository root, run `./scripts/package.sh`. It builds `dist/kobo-dashboard/bin/dashboard` for Linux ARMv7 and `dist/setup` for the build computer. The Go toolchain and module downloads are needed on the build computer only. The installed Kobo needs no Go, Python, Node, or computer server.

The package automatically includes the official FBInk Kobo CI build from source commit `886f25f13368859ad8a899b88d04c26e19cda32e`, verified against GitHub's SHA-256 digest. The download requires an authenticated `gh` CLI on the build computer (GitHub requires authentication for public Actions artifacts); cached artifacts work offline. `scripts/fetch-fbink.sh` pins the artifact, includes its license, credits, and matching CLI source with compiled dependencies. The build supports Condor/Elipsa 2E and PNG. Its ARM hard-float loader and libc/libm requirements must exist on the actual Kobo. Required GLIBC symbols are version 2.4. Runtime execution remains untested on this device.

You can override the binary using `FBINK_BINARY=/absolute/path/to/fbink ./scripts/package.sh`; you must supply a compatible full-image build and its required redistribution materials. Do not use KOReader's minimal FBInk binary: its image support is disabled. Artifact expiry or download/checksum failure stops packaging instead of producing an incomplete install. Preserve the verified build cache for future rebuilds. No downloaded installer or KoboRoot archive is executed.

The package copies the build computer's public `/etc/ssl/cert.pem` CA roots to `config/cert.pem` and the launcher sets `SSL_CERT_FILE` so Go HTTPS works independently of the Kobo's old trust store. Use `CA_BUNDLE=/path/to/maintained/cert.pem` to override the source; this must contain public CA certificates, never private keys. Refresh the bundle when shipping app updates.

Use [NickelMenu](https://github.com/pgaskin/NickelMenu) only after checking compatibility with the exact installed firmware. This launch integration is limited to firmware 4.x; it refuses other versions. It does not patch firmware or bypass compatibility checks. Firmware 4.x alone does not guarantee every NickelMenu release is compatible.

## Before copying dashboard files

The dashboard archive does **not** install NickelMenu. Its `nickelmenu/kobo-dashboard` file is only a menu entry. Creating `.adds` manually is fine, but cannot create the NickelMenu menu by itself.

1. Check the exact firmware first. This integration requires 4.x; NickelMenu currently states that 5.x is unsupported.
2. Follow the [official NickelMenu installation](https://pgaskin.net/NickelMenu/): download its `KoboRoot.tgz` to `.kobo/KoboRoot.tgz`, eject, and let installation/reboot finish. Confirm **NickelMenu itself is visible** before copying the dashboard. If it is absent, stop here and diagnose the launcher installation.
3. Before adding custom folders, back up `.kobo/Kobo/Kobo eReader.conf` to the computer. Kobo firmware 4.17+ can index hidden directories. The [KOReader installation guide](https://github.com/koreader/koreader/wiki/Installation-on-Kobo-devices) recommends excluding custom hidden directories with this setting:

```ini
[FeatureSettings]
ExcludeSyncFolders=(\\.(?!kobo|adobe).+|([^.][^/]*/)+\\..+)
```

The backslashes above are literal. If `[FeatureSettings]` already exists, add the key within that section rather than creating another section. If `ExcludeSyncFolders` already exists, preserve its existing exclusions and review the merge; do not blindly replace it. This is preventive guidance, not proof that indexing caused a particular slowdown.

If the device becomes slow after copying files and NickelMenu is absent, reconnect USB and move **only** `.adds/kobo-dashboard` and `.adds/nm/kobo-dashboard` back to the computer. Preserve other `.adds` contents, books, and `.kobo`. Eject, perform a normal restart, and check whether performance recovers before installing anything else. The app has no automatic boot entry in this package and is launched only by selecting its menu item. A missing menu alone does not explain a slowdown; inspect firmware, copied paths, configuration and any existing logs before assigning a cause.

## USB install

1. Connect to home Wi-Fi using the normal Kobo interface first, so Nickel has a saved WPA network and loaded the Wi-Fi modules. Turn its frontlight off for the battery target.
2. Mount the Kobo by USB. Copy `dist/kobo-dashboard` to `.adds/kobo-dashboard` on its visible storage volume. Copy `dist/nickelmenu/kobo-dashboard` into `.adds/nm/` after NickelMenu is installed.
3. Complete `docs/google-setup.md` on the computer. Copy the resulting private credentials to `.adds/kobo-dashboard/data/credentials.json`. Do not add it to git, a release archive, screenshots, or logs. Configuration and credentials are ordinary files on user-accessible storage; this is not encrypted secure storage.
4. Eject the volume cleanly. Select **Home dashboard** in NickelMenu. Its launcher pauses Nickel and restores Wi-Fi before resuming it on normal exit, error, or signal. Wi-Fi recovery has a 45-second watchdog. Reboot always starts Nickel normally. Dashboard updates stop when the dashboard process exits.

`launch.sh` creates a `data/launcher.lock` directory to prevent two instances. After a forced power loss, remove a stale directory over USB only after verifying the dashboard is no longer running. Logs are in `data/launcher.log`. They must not contain OAuth tokens.

The default launch is an **awake hardware validation mode**: real rendering and touch, but automatic suspend is disabled. Once the checks below pass, create a trusted local `device.env` containing `DASHBOARD_EXTRA_ARGS='--suspend'`. This local shell configuration can also override `TOUCH_PATH`, `POWER_PATH`, `TOUCH_MAX_X`, and `TOUCH_MAX_Y` when automatic capability discovery is insufficient. Never source untrusted files.

## Device checks

Run `dashboard --doctor --fbink /mnt/onboard/.adds/kobo-dashboard/bin/fbink` on the device to inspect firmware, framebuffer dimensions, battery state, RTC name, input names and resolved touch/power paths. Doctor is read-only and never starts Wi-Fi or suspends. Save its output without private credentials. An absent or unsupported prerequisite must be resolved before battery operation.

Automatic input discovery examines Linux evdev capabilities (BTN_TOUCH and X/Y or multitouch X/Y; KEY_POWER for the power button), then reads raw coordinate maxima with `EVIOCGABS`. The native ARM input-event ABI is 16 bytes, rather than the 24 bytes used by 64-bit desktop systems. Only the primary touch slot is used. Elipsa 2E portrait coordinates mirror Y based on KOReader's Condor profile. Test all four corners and every visible control; use raw maximum overrides if required. Rotation and stylus interaction are not validated.

The framebuffer is assumed to be 1404 × 1872 in portrait. FBInk performs the driver-specific e-ink update. Inspect every area for clipping and ghosting; a full flashing refresh is requested for each snapshot. Clear a day dialog and confirm the underlying grid is restored.

## Battery operation and safeguards

The implemented suspend path follows KOReader's Kobo pattern: set RTC wake alarm in UTC through `/dev/rtc0` using `RTC_WKALM_SET`, read it back, disable Wi-Fi, set `/sys/power/state-extended` to `1`, wait two seconds, sync, write `mem` to `/sys/power/state`, and restore state-extended to `0` after waking. The alarm is disabled afterward. Unsupported RTC ioctls produce an error, not an emulated sleep. Successful calls alone do not prove that the kernel actually suspended; observe wake behavior and measure power.

**MediaTek suspend while plugged into USB can hang the kernel.** Suspend is refused unless `/sys/class/power_supply/bd71827_bat/status` explicitly reads `Discharging`, and that state is checked again immediately before suspend. Unknown status, charging, and fully charged USB states all prevent suspension. This check is not permission to attach USB while suspend is entering; stop dashboard battery validation before connecting USB.

The supplied Wi-Fi helpers support the narrow MediaTek `wlan_drv_gen4m` configuration with `/dev/wmtWifi`, `/proc/driver/wmt_dbg`, saved `/etc/wpa_supplicant/wpa_supplicant.conf`, `wpa_cli`, `wpa_supplicant`, `ifconfig`, and `/sbin/dhcpcd`. They use the upstream power-control sequence and never unload MediaTek modules. Missing helpers or prerequisites are explicit errors. No Wi-Fi password is requested or copied into dashboard settings. If a device uses different interfaces, drivers, or DHCP tools, these helpers need a reviewed device-specific adaptation before unattended operation.

Before enabling unattended hourly mode, verify:

- A snapshot remains visible across sleep, including after the sleep cover is closed/opened.
- The power button resumes controls and every touch maps correctly.
- At least three RTC-driven hourly cycles fetch real data, redraw, and resume sleep.
- A Wi-Fi failure leaves cached widgets visible and the next scheduled cycle recovers.
- Manual refresh and editor pairing work after wake; closing/expiring the editor allows sleep.
- Returning to Nickel restores reading, handwriting, Wi-Fi, and normal power behavior.
- A seven-day battery measurement with frontlight off meets the target. Record firmware and charge levels; do not infer endurance from a short test.

## Primary source references

- [KOReader Condor device profile and suspend precautions](https://github.com/koreader/koreader/blob/master/frontend/device/kobo/device.lua): Elipsa 2E, MediaTek, mirrored Y, battery sysfs, state-extended suspend sequence, and plugged-in kernel hang precaution.
- [KOReader RTC implementation](https://github.com/koreader/koreader-base/blob/master/ffi/rtc.lua): `/dev/rtc0`, UTC, `RTC_WKALM_SET` and alarm interrupt management.
- [Linux RTC UAPI](https://github.com/torvalds/linux/blob/master/include/uapi/linux/rtc.h): fixed 40-byte wake-alarm structure and ioctl numbers.
- [KOReader Wi-Fi enable](https://github.com/koreader/koreader/blob/master/platform/kobo/enable-wifi.sh) and [disable](https://github.com/koreader/koreader/blob/master/platform/kobo/disable-wifi.sh): MediaTek WMT sequence, nl80211, and no module unloading.
- [FBInk CLI](https://github.com/NiLuJe/FBInk/blob/master/CLI.md) and [NickelMenu action configuration](https://github.com/pgaskin/NickelMenu/blob/master/res/doc).

These upstream references were inspected during implementation. Device-specific behavior still needs verification on the user's exact firmware.
