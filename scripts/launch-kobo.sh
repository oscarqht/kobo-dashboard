#!/bin/sh
# Explicitly launched by NickelMenu. Resume Nickel even if the app fails.
set -eu
APP=/mnt/onboard/.adds/kobo-dashboard
LOCK="$APP/data/launcher.lock"
mkdir -p "$APP/data"
if ! mkdir "$LOCK" 2>/dev/null; then exit 0; fi
NICKEL_PID=""
cleanup() {
 if [ -n "$NICKEL_PID" ]; then
  if [ -x "$APP/config/wifi-on.sh" ]; then
   "$APP/config/wifi-on.sh" >> "$APP/data/launcher.log" 2>&1 &
   wifi_pid=$!
   ( sleep 45; kill -TERM "$wifi_pid" 2>/dev/null || true ) &
   watchdog_pid=$!
   wait "$wifi_pid" || true
   kill "$watchdog_pid" 2>/dev/null || true
  fi
  kill -CONT "$NICKEL_PID" 2>/dev/null || true
 fi
 rmdir "$LOCK" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
VERSION=$(cut -d, -f3 /mnt/onboard/.kobo/version)
case "$VERSION" in
 4.*) ;;
 *) printf '%s\n' "Unsupported firmware $VERSION: launcher limited to reviewed NickelMenu 4.x integration." >> "$APP/data/launcher.log"; exit 1 ;;
esac
if [ ! -x "$APP/bin/fbink" ]; then printf '%s\n' 'Missing bin/fbink; see DEVICE.md.' >> "$APP/data/launcher.log"; exit 1; fi
# /proc lookup avoids substring matching another process or killing the shell.
for entry in /proc/[0-9]*/comm; do
 if [ "$(cat "$entry" 2>/dev/null || true)" = nickel ]; then
  pid=${entry#/proc/}; NICKEL_PID=${pid%/comm}; break
 fi
done
if [ -z "$NICKEL_PID" ]; then printf '%s\n' 'Nickel process missing; refusing to launch.' >> "$APP/data/launcher.log"; exit 1; fi
export SSL_CERT_FILE="$APP/config/cert.pem"
cd "$APP"
# device.env is local trusted shell configuration. Never include it in releases.
if [ -f "$APP/device.env" ]; then . "$APP/device.env"; fi
kill -STOP "$NICKEL_PID"
# Deliberately do not exec: the EXIT trap must resume Nickel on every app exit.
"$APP/bin/dashboard" --device --data "$APP/data" --fbink "$APP/bin/fbink" \
 --touch-device "${TOUCH_PATH:-}" --power-device "${POWER_PATH:-}" \
 --touch-max-x "${TOUCH_MAX_X:-0}" --touch-max-y "${TOUCH_MAX_Y:-0}" \
 --mirror-y=true --wifi-on "$APP/config/wifi-on.sh" --wifi-off "$APP/config/wifi-off.sh" ${DASHBOARD_EXTRA_ARGS:-} >> "$APP/data/launcher.log" 2>&1
