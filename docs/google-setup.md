# Connect your personal Google Calendar

The dashboard fetches Calendar data directly from Google. No companion computer or server stays running. A computer is needed for first authorization and again if Google revokes or expires that authorization. Read-only access covers your calendar list and events; the app stores and displays event titles and date boundaries, not event descriptions, locations, or attendees.

## Create your personal OAuth client

1. Open [Google Cloud Console](https://console.cloud.google.com/) and create a project for your personal dashboard.
2. Enable **Google Calendar API** in APIs & Services → Library.
3. Configure Google Auth Platform branding, audience, and data access. Choose External if using a personal Google account. Add `https://www.googleapis.com/auth/calendar.readonly` as the scope.
4. For initial testing, add your Google account as a test user. To avoid the normal seven-day refresh-token lifetime for an External app in Testing that requests Calendar access, change its publishing status to **In production** before your final authorization. This does not make your local dashboard public. Personal-use apps may qualify for the personal-use verification exception; Google can still show an unverified-app warning. Use only a project and account you control. Organization administrators may block authorization.
5. Create an OAuth client with application type **Desktop app**. Download its JSON file somewhere private outside this repository. A Web application client will not work with this helper.

The helper cannot automate Cloud Console setup or obtain permission on your behalf. Google's account policies and UI may change; consult the official references below if an option differs.

## Authorize and transfer over USB

On your computer, from this repository:

```sh
go run ./src/cmd/setup -client /private/path/desktop-client.json -output /private/path/credentials.json
```

Your normal browser opens Google's consent screen. The helper listens only on a temporary `127.0.0.1` port and uses an unpredictable state value and PKCE S256. Consent requests offline, read-only Calendar access. Return to the terminal after approving it. If opening the browser fails, copy the displayed authorization URL into a browser on the **same computer**. `-no-browser` skips opening it automatically.

The output is created with mode `0600`. Existing files are never overwritten. It contains a refresh token: treat it as a password and never commit, email, or upload it. It is not a general Google login session.

Copy the output over USB into the installed dashboard's private data directory as `credentials.json` (normally `.adds/kobo-dashboard/data/credentials.json`). Alternatively the dashboard's `--credentials /path/to/credentials.json` option imports it into its data directory. Keep the file off shared storage and remove unnecessary transfer copies afterward. USB mass storage exposes files to a connected computer; filesystem permissions do not encrypt them.

Launch the dashboard, wake its controls, and open the paired local editor to select calendars. The Kobo obtains temporary access tokens directly from Google using its saved refresh token. It does not need the helper for hourly updates.

## Reconnect or disconnect

If a widget reports that authorization expired or was revoked, repeat setup with a new output path and replace the private Kobo credentials file while the dashboard is stopped. If Google returns no refresh token, remove the app's existing access in your Google account connections, then authorize again. Do not delete a working token until its replacement is ready.

To disconnect, revoke the app in [Google account connections](https://myaccount.google.com/connections), remove the Kobo's `credentials.json`, and remove its cached calendar data if you also want the old snapshot erased. Testing status, password/security changes, revocation, inactivity, token limits, and organization policy can all require reauthorization. Production status avoids the Testing-specific seven-day rule; it does not guarantee a permanent token.

## Official references

- [Google desktop OAuth: PKCE, loopback redirect, offline access, and token refresh](https://developers.google.com/identity/protocols/oauth2/native-app)
- [Google token expiration and revocation](https://developers.google.com/identity/protocols/oauth2#expiration)
- [Personal-use verification exception](https://support.google.com/cloud/answer/13464323)
- [Calendar events API: expanded recurring instances and pagination](https://developers.google.com/workspace/calendar/api/v3/reference/events/list)
- [Calendar list API](https://developers.google.com/workspace/calendar/api/v3/reference/calendarList/list)

## Weather service

Weather uses [Open-Meteo forecast](https://open-meteo.com/en/docs) and [geocoding](https://open-meteo.com/en/docs/geocoding-api) endpoints directly, without an API key for this personal noncommercial use. Attribution should be visible as **Weather data by Open-Meteo.com**; geocoding data is based on GeoNames. Open-Meteo's free service has usage limits and no availability guarantee. Hourly requests for a personal dashboard are small; review its current terms before distributing or commercializing the app.
