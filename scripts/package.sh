#!/bin/sh
# Build locally; does not connect to, mount, or modify a Kobo.
set -eu
cd "$(dirname "$0")/.."
DEST="${1:-dist}"
if [ -d "$DEST/kobo-dashboard/data" ] && [ -n "$(ls -A "$DEST/kobo-dashboard/data")" ]; then
 printf '%s\n' 'Refusing to package an existing data directory: choose a fresh output path.' >&2
 exit 1
fi
mkdir -p "$DEST/kobo-dashboard/bin" "$DEST/kobo-dashboard/data" "$DEST/nickelmenu" "$DEST/kobo-dashboard/config"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags="-s -w" -o "$DEST/kobo-dashboard/bin/dashboard" ./src/cmd/dashboard
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$DEST/setup" ./src/cmd/setup
CERT_SOURCE="${CA_BUNDLE:-/etc/ssl/cert.pem}"
test -s "$CERT_SOURCE"
cp "$CERT_SOURCE" "$DEST/kobo-dashboard/config/cert.pem"
cp config/wifi-on.sh config/wifi-off.sh "$DEST/kobo-dashboard/config/"
chmod 755 "$DEST/kobo-dashboard/config/"*.sh
cp scripts/launch-kobo.sh "$DEST/kobo-dashboard/launch.sh"
cp config/nickelmenu "$DEST/nickelmenu/kobo-dashboard"
cp docs/device.md "$DEST/DEVICE.md"
cp docs/google-setup.md "$DEST/GOOGLE-SETUP.md"
cp docs/architecture.md "$DEST/ARCHITECTURE.md"
mkdir -p "$DEST/kobo-dashboard/notices"
go_root=$(go env GOROOT)
go_license="$go_root/LICENSE"
if [ ! -f "$go_license" ]; then go_license="$go_root/../LICENSE"; fi
cp "$go_license" "$DEST/kobo-dashboard/notices/Go-LICENSE"
for dependency in golang.org/x/image golang.org/x/text; do
 module_dir=$(go list -m -f '{{.Dir}}' "$dependency")
 label=$(basename "$dependency")
 install -m 644 "$module_dir/LICENSE" "$DEST/kobo-dashboard/notices/$label-LICENSE"
done
chmod 755 "$DEST/kobo-dashboard/launch.sh"
if [ -n "${FBINK_BINARY:-}" ]; then
 test -f "$FBINK_BINARY"
 cp "$FBINK_BINARY" "$DEST/kobo-dashboard/bin/fbink"
 chmod 755 "$DEST/kobo-dashboard/bin/fbink"
else
 "./scripts/fetch-fbink.sh" "$DEST/kobo-dashboard/bin"
fi
rm -f "$DEST/Paper-Kobo-Elipsa-2E.zip"
(cd "$DEST" && zip -q -r Paper-Kobo-Elipsa-2E.zip kobo-dashboard nickelmenu setup DEVICE.md GOOGLE-SETUP.md ARCHITECTURE.md)
printf '%s\n' "Built $DEST/Paper-Kobo-Elipsa-2E.zip. Read DEVICE.md before copying files. Credentials are not packaged."
