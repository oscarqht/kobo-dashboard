#!/bin/sh
# Pin an official upstream Kobo CI build, verify its GitHub-published digest,
# and keep license + matching CLI source alongside the executable.
set -eu
DEST=${1:?pass destination directory}
CACHE=${FBINK_CACHE:-${TMPDIR:-/tmp}/kobo-dashboard-fbink-8951879128}
mkdir -p "$CACHE" "$DEST"
ARCHIVE="$CACHE/artifact.zip"
EXPECTED=b9116bf4e29a37302e27f83060f9c1c340119531553391eaeabebbdd6a2ae3eb
if [ ! -f "$ARCHIVE" ]; then
 if ! command -v gh >/dev/null; then echo 'FBInk download requires gh authenticated for public Actions artifacts, or set FBINK_BINARY to a verified full-image Kobo build.' >&2; exit 1; fi
 gh api repos/NiLuJe/FBInk/actions/artifacts/8951879128/zip > "$ARCHIVE.partial"
 mv "$ARCHIVE.partial" "$ARCHIVE"
fi
if command -v sha256sum >/dev/null; then ACTUAL=$(sha256sum "$ARCHIVE" | cut -d' ' -f1); else ACTUAL=$(shasum -a 256 "$ARCHIVE" | cut -d' ' -f1); fi
if [ "$ACTUAL" != "$EXPECTED" ]; then echo 'FBInk artifact checksum mismatch; refusing to package.' >&2; exit 1; fi
WORK=$(mktemp -d "${TMPDIR:-/tmp}/dashboard-fbink.XXXXXX")
trap 'rm -rf "$WORK"' EXIT
unzip -p "$ARCHIVE" FBInk-Kobo-886f25f.tar.gz > "$WORK/build.tar.gz"
tar -xzf "$WORK/build.tar.gz" -C "$WORK" ./fbink ./LICENSE ./CREDITS
cp "$WORK/fbink" "$DEST/fbink"
cp "$WORK/LICENSE" "$DEST/FBInk-LICENSE"
cp "$WORK/CREDITS" "$DEST/FBInk-CREDITS"
chmod 755 "$DEST/fbink"
COMMIT=886f25f13368859ad8a899b88d04c26e19cda32e
if [ ! -f "$CACHE/FBInk-source.tar.gz" ]; then
 mkdir -p "$WORK/source"
 curl -fsSL "https://codeload.github.com/NiLuJe/FBInk/tar.gz/$COMMIT" -o "$WORK/source.tar.gz"
 tar -xzf "$WORK/source.tar.gz" --strip-components=1 -C "$WORK/source"
 # Dependencies compiled into this CLI (not the unshipped i2c/libevdev utilities).
 for item in 'font8x8 NiLuJe/font8x8 fa74831b9076805271cdedc0ee12137aa548e524' 'stb NiLuJe/stb dfa32facac4a16184dd66f8e482bf15a678f2bf3' 'libunibreak adah1972/libunibreak 3ce4bfa3129ff3738046a44a6db533d2ce25af2b'; do
  set -- $item
  mkdir -p "$WORK/source/$1"
  curl -fsSL "https://codeload.github.com/$2/tar.gz/$3" -o "$WORK/module.tar.gz"
  tar -xzf "$WORK/module.tar.gz" --strip-components=1 -C "$WORK/source/$1"
 done
 tar -czf "$CACHE/FBInk-source.tar.gz" -C "$WORK" source
fi
cp "$CACHE/FBInk-source.tar.gz" "$DEST/FBInk-source.tar.gz"
cat > "$DEST/FBInk-SOURCE.txt" <<'NOTICE'
FBInk official Kobo GitHub Actions artifact 8951879128
SHA256 b9116bf4e29a37302e27f83060f9c1c340119531553391eaeabebbdd6a2ae3eb
Source: https://github.com/NiLuJe/FBInk/tree/886f25f13368859ad8a899b88d04c26e19cda32e
License: GPL-3.0-or-later; see FBInk-LICENSE and FBInk-CREDITS.
Matching CLI source and its compiled dependencies: FBInk-source.tar.gz.
Build workflow/toolchain: upstream .github/workflows at the source commit.
Binary is ARM EABI5; loader /lib/ld-linux-armhf.so.3, libc.so.6 and libm.so.6.
Required GLIBC symbol version: GLIBC_2.4. Includes Condor/Elipsa 2E + PNG.
Artifact availability may expire. Preserve this verified artifact for rebuilding.
This build has not been executed on the user's Kobo.
NOTICE
