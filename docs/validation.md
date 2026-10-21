# Validation record

Developed and checked on macOS/Apple Silicon, 21 October 2026. No physical Kobo or Google account authorization was available.

## Verified locally

- `go test -race ./...` and `go vet ./...`.
- Linux/ARMv7 cross-build of the application and Linux-specific device tests.
- Provider HTTP fixtures: read-only Google scopes, PKCE/state, recurring expanded paginated calendars/events, offline refresh, authorization failure, malformed upstream responses, location lookup and weather validation.
- Live Open-Meteo location search returned Singapore coordinates through the running editor API.
- Cache integration: one failed calendar retains its previous events while another calendar and weather update; failed weather retains its own prior snapshot. Changed location, units or timezone invalidates incompatible weather data.
- Native runtime simulation: the actual touch-dispatch loop presents PNGs, switches calendar mode, shuts down the radio after fetching, returns to the reader, and exits on removed input. Fake devices exercise logic, not physical behavior.
- Security boundaries: unauthorized state/snapshot rejection, pairing throttling, inactivity expiration, cross-origin/host rejection, strict request decoding, no credentials or provider response secrets in public state/cache. Automatic GET polling does not keep an editor session awake.
- Native rendering: PNG dimensions, Monday-first week/month boundaries, leap months, timezone/all-day end-exclusive events, title wrapping/overflow, full day pagination, touch bounds, weather at minimum widget dimensions, modal input isolation.
- Browser: desktop editor renders native snapshot, week/month preset gives a draft PNG before Save, saving persists configuration, calendar resize and duplication produce separate nonoverlapping instances. Local preview uses explicit sample data.
- Bundle: static ARM application, host setup helper, checksum-pinned full-image FBInk/Condor build with matching source and licenses, public CA bundle, launcher and Wi-Fi scripts. Scripts pass syntax checking.

## Physical/account checks still required

Exact firmware compatibility, native executable/loader execution, framebuffer orientation and touch calibration, Wi-Fi reconnect after boot, RTC-driven hourly wake, retained snapshot during suspend, charging behavior, return to Nickel/handwriting, real Google consent/token issuance, and seven-day battery endurance.

These cannot be established by a desktop preview, mocks or cross-compilation. The device launcher intentionally starts with suspend disabled for hardware validation; enable `--suspend` after the documented checks. No claim of one-week endurance is made.

## Coverage limits

Provider and rendering suites cover over 90% of their statements, OAuth callback branches and session-protection/JSON decoding are fully exercised. Overall macOS statement coverage is below the inherited 80% target because Linux device paths, real setup-browser login, and some executable startup paths are not executed on macOS. Linux-only safety tests are cross-compiled but not runtime-executed here; the local Docker daemon was unavailable. Coverage of desktop logic does not substitute for hardware validation.
