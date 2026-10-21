#!/bin/sh
# Narrow MediaTek poweroff. Never unload MediaTek Wi-Fi kernel modules.
set -eu
case "$(cut -d, -f3 /mnt/onboard/.kobo/version)" in 4.*) ;; *) echo 'Wi-Fi: unsupported firmware' >&2; exit 1;; esac
test -c /dev/wmtWifi
grep -q '^wlan_drv_gen4m ' /proc/modules
test -x /sbin/dhcpcd
for tool in wpa_cli ifconfig; do command -v "$tool" >/dev/null; done
backup=$(mktemp /tmp/dashboard-resolv.XXXXXX)
cp /etc/resolv.conf "$backup"
cleanup() { if [ ! -s /etc/resolv.conf ]; then cp "$backup" /etc/resolv.conf; fi; rm -f "$backup"; }
trap cleanup EXIT
# A missing DHCP process or supplicant is already the intended disabled state.
/sbin/dhcpcd -d -k wlan0 2>/dev/null || true
wpa_cli -i wlan0 terminate 2>/dev/null || true
ifconfig wlan0 down
printf '%s\n' 0 >/dev/wmtWifi
