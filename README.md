# Paper — Kobo home dashboard

A native home dashboard for Kobo Elipsa 2E: weather, read-only Google Calendar, and a customizable widget layout. It runs independently after USB setup, with a retained e-ink snapshot between refreshes.

## Try the desktop preview

Requires Go 1.24 or newer:

```sh
go run ./src/cmd/dashboard --demo --data ./data/demo
```

Open **http://127.0.0.1:8787**. The preview uses clearly labeled sample calendar/weather data. Try the week/month presets, arrange widgets, duplicate instances, choose calendars and preview changes before Save. The preview renders the same PNG as the native app; it does not establish hardware compatibility.

For real providers on the computer, run without `--demo` and use a separate `--data` directory with your private authorization file.

## Install on Kobo

A build is available in `dist/`. To rebuild:

```sh
./scripts/package.sh
```

The bundle includes a static Linux/ARMv7 executable, full FBInk with pinned checksum and corresponding source/license, Wi-Fi helpers, public CA roots, NickelMenu launch configuration, and a setup helper for the build computer. Initial dependency fetching needs network and authenticated `gh` access; subsequent builds can use the verified cache. Credentials are never included.

**Install NickelMenu separately and confirm its menu is visible before copying dashboard files.** Creating `.adds` or copying the supplied menu-entry file does not install NickelMenu. Follow the hidden-folder indexing precautions in [device installation](docs/device.md), then [Google connection setup](docs/google-setup.md). The launcher supports reviewed firmware 4.x integration; check your exact firmware before installing NickelMenu. Google login occurs in your computer browser once, with USB authorization transfer. Normal operation needs no computer/server. Revoked authorization requires reconnecting with the helper.

**Device validation is still required.** No Kobo is currently attached. Native builds and local tests pass, but touch, retained sleep images, hourly RTC wake/Wi-Fi recovery, and battery endurance have not been physically tested. The launcher initially runs awake for validation. Enable guarded sleep after device checks; the target is one week with the frontlight off. MediaTek suspend is refused while charging or power state is unknown.

## Included behavior

- Weather: location search, Celsius/Fahrenheit, current conditions, high/low, rain probability, three-day forecast.
- Calendar: independently configured week/month instances, Monday–Sunday grids, titles only, day detail pagination and overflow indicators.
- Paired same-Wi-Fi editor served temporarily by the Kobo; drag/resize grid, presets, multiple instances, explicit Save.
- Refresh hourly, on entry and manually; retain each source's last successful data on failures.
- Power-button wake, inactivity sleep, and return to normal Kobo reading. Reboot starts the normal Kobo interface.
- Documented compiled widget contract for future functionality.

Weather data: [Open-Meteo](https://open-meteo.com/); location search: [GeoNames](https://www.geonames.org/).

## Validate

```sh
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build ./src/cmd/dashboard
```

The tests cover OAuth state/PKCE and token handling, paginated providers, independent cache failures, configuration validation, session security, timezone/calendar boundaries, PNG/touch geometry, and input/RTC ABI parsing. Live Google consent and hardware endurance require your account/device.

See [architecture and adding widgets](docs/architecture.md).
