#!/bin/sh
# Narrow MTK/Condor bringup following KOReader platform/kobo/enable-wifi.sh.
# Modules must already be loaded by Nickel; no driver loading or unloading here.
set -eu
case "$(cut -d, -f3 /mnt/onboard/.kobo/version)" in 4.*) ;; *) echo 'Wi-Fi: unsupported firmware' >&2; exit 1;; esac
test -c /dev/wmtWifi
test -w /proc/driver/wmt_dbg
grep -q '^wlan_drv_gen4m ' /proc/modules
for tool in ifconfig wpa_supplicant wpa_cli; do command -v "$tool" >/dev/null; done
test -x /sbin/dhcpcd
test -r /etc/wpa_supplicant/wpa_supplicant.conf
# Idempotent while already connected: do not power-cycle an editor session.
if wpa_cli -i wlan0 status 2>/dev/null | grep -q 'wpa_state=COMPLETED'; then
 if ifconfig wlan0 | grep -q 'inet addr:'; then exit 0; fi
 /sbin/dhcpcd -d -t 12 wlan0
 exit 0
fi
printf '%s\n' 0xDB9DB9 >/proc/driver/wmt_dbg
printf '%s\n' '7 9 0' >/proc/driver/wmt_dbg
sleep 1
printf '%s\n' 0xDB9DB9 >/proc/driver/wmt_dbg
printf '%s\n' '7 9 1' >/proc/driver/wmt_dbg
printf '%s\n' 1 >/dev/wmtWifi
sleep 1
ifconfig wlan0 up
if ! wpa_cli -i wlan0 ping 2>/dev/null | grep -q PONG; then
 wpa_supplicant -D nl80211 -s -i wlan0 -c /etc/wpa_supplicant/wpa_supplicant.conf -C /var/run/wpa_supplicant -B
fi
count=0
until wpa_cli -i wlan0 status 2>/dev/null | grep -q 'wpa_state=COMPLETED'; do
 count=$((count + 1)); if [ "$count" -ge 20 ]; then echo 'Wi-Fi: association timed out; connect using Nickel first' >&2; exit 1; fi
 sleep 1
done
/sbin/dhcpcd -d -t 12 wlan0
