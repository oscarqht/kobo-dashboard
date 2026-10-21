# Architecture and widget contract

The Kobo runs one native Linux/ARMv7 Go process. It fetches directly from Google Calendar and Open-Meteo, persists configuration and per-source snapshots, renders a 1404 × 1872 PNG, and asks the bundled FBInk CLI to update the e-ink framebuffer. No browser, JavaScript engine, remote renderer, or companion server runs on the device for normal display operation.

## Components

- `src/internal/model`: versioned JSON contracts, grid validation, source status identifiers.
- `src/internal/providers`: read-only Google Calendar, PKCE setup primitives, weather and location search. HTTP clients are bounded, non-redirecting, and errors exclude response secrets.
- `src/internal/app`: persistent state, independent source refreshes, view navigation, temporary paired editor API. Credentials have a separate private file and never enter configuration/state responses.
- `src/internal/render`: native Go image/font drawing, Monday-first calendar arithmetic, end-exclusive events, touch rectangles and day pagination.
- `src/internal/device`: framebuffer presentation through FBInk, evdev discovery/calibration, MediaTek Wi-Fi helpers, guarded RTC wake/suspend.
- `src/internal/webui`: offline assets embedded into the executable. The editor is opened explicitly on the Kobo, pairs with a six-digit code, and closes after ten minutes without user activity. Automatic polls do not extend the session.
- `src/cmd/setup`: desktop-only OAuth helper. It uses the computer's system browser, loopback callback, state and S256 PKCE, then exports authorization for USB transfer.

## Widget contract

`model.Widget` is the version-one configuration contract. Every instance has a unique `id`, `type`, and integer `x/y/w/h` rectangle in a 12 × 12 grid. Coordinates are zero-based in storage, one-based in the editor. Rectangles may not overlap or exceed the grid. Calendar minimum size is 6 × 4, weather 4 × 2; at most sixteen instances are accepted.

Built-in settings:

- `calendar`: `mode` is `week` or `month`; `calendar_ids` chooses independently for each instance. `primary` is Google's primary-calendar alias. Instances share fetched event data by calendar ID, but retain separate display modes and temporary navigation offsets.
- `weather`: saved location label, latitude/longitude, and `units` (`celsius` or `fahrenheit`). Each widget gets an independent weather cache and status.

`model.Snapshot` holds display data, separate from credentials. `Status` stores last successful update and a sanitized error. Keys use `calendar:<source-id>` and `weather:<widget-id>` to prevent collisions. A failed fetch preserves its source's previous values; successful calendars replace their previous event set, so deleted/cancelled events disappear. Changing a weather widget's location, units, or dashboard timezone invalidates its incompatible old cache.

Native rendering accepts `(Config, Snapshot, View)` and returns PNG bytes. `render.Hits` returns pixel rectangles carrying `Action`, `WidgetID`, and `Value`; device touch and desktop preview dispatch the same actions. Navigation is temporary and resets when sleeping. Layout/mode settings persist.

## Adding a widget through an app update

1. Add typed settings/data to the model, and extend `Config.Validate` with valid type, minimum dimensions and settings bounds. Add a config migration when changing the persisted schema.
2. Add a bounded provider in `providers`, or reuse an existing source. Integrate it into `Engine.Refresh`, retaining prior data on failure and recording successful timestamps independently. Avoid holding the state lock during network requests.
3. Add a rendering function and dispatch in `render.build`. Restrict drawing and touch targets to the widget's rectangle. Provide a useful unconfigured/error state.
4. Add editor controls and defaults in `webui/editor.js`. Use DOM text nodes for external strings, not HTML injection; save only public settings. Extend duplicate/resize placement rules.
5. Add provider failure/cache tests, rendering/date/geometry checks, and an editor flow check. Rebuild the native bundle and validate on the exact firmware.

Widgets are compiled modules delivered with app updates. There is no runtime third-party plugin loader in version one.

## Persistence and security boundaries

Configuration and cache are written through same-directory temporary files, synced and renamed. Default file modes are 0600 and directory modes 0700, subject to the Kobo storage filesystem's permission support. USB-visible storage is not encrypted secure storage. Never share `credentials.json`, client downloads or real cache exports.

The same-Wi-Fi editor uses local HTTP, a random HttpOnly/SameSite cookie, strict host/origin checks, custom mutation headers, request-size limits and pairing throttling. It is intended for a trusted home network. No OAuth token is sent to the editor. Build-generated CA roots preserve HTTPS verification for external providers.

See `device.md` for exact hardware assumptions and the validation boundary. Hourly RTC operation and one-week battery endurance require actual device evidence.
