# concept2upload

Version 0.5.1 (see [CHANGELOG.md](CHANGELOG.md) for release notes).

A small Go CLI that talks to the [Concept2 Logbook API](https://log.concept2.com/developers/documentation/)
to list your recent ergometer workouts and download one at a time as a
Garmin-compatible `.tcx` file (heart rate, cadence, and — for
rower/SkiErg/dynamic pieces with stroke data — estimated watts).

It can also upload a downloaded workout straight to Strava, using
Strava's own upload API and OAuth flow.

## Prerequisites

- A workout recorded on a Concept2 erg (RowErg, BikeErg, SkiErg, or
  Dynamic) using the **ErgData** app on your phone, paired to the machine
  over Bluetooth, with that workout synced to your online
  [Concept2 Logbook](https://log.concept2.com/). This tool reads from your
  Logbook account, not from the erg or the app directly, so the workout
  needs to have made it there first.
- A Concept2 Logbook API access token — see [Requirements](#requirements)
  below for how to get one.

## Install

### Homebrew (macOS/Linux)

```bash
brew install --cask eoinaokane/tap/concept2upload
```

### From source

Requires Go 1.22+:

```bash
git clone https://github.com/eoinaokane/concept2upload.git
cd concept2upload
make build        # builds ./dist/concept2upload
```

## Quick start

```bash
concept2upload auth-concept2 your-access-token   # one-time, see Requirements below
concept2upload list                     # see your recent workouts, numbered
concept2upload get 1                    # download workout #1 as a .tcx file
```

See [Usage](#usage) below for the rest of the commands.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for release history. Known gaps and
planned work are tracked as
[GitHub issues](https://github.com/eoinaokane/concept2upload/issues).

## Requirements

A Concept2 Logbook API access token. For personal use (this tool talks to
your own account only), the simplest way to get one is a self-service
long-lived token rather than registering a full OAuth app:

1. Log in at [log.concept2.com](https://log.concept2.com/).
2. Go to **Edit Profile > Applications > Concept2 Logbook API
   integration**.
3. Generate a long-lived authorization token there and copy it.

(Registering an OAuth application — for apps serving multiple users — is
also documented on the [developer docs](https://log.concept2.com/developers/documentation/)
site, but isn't needed for this tool.)

## Usage

The examples below use `./dist/concept2upload` (a from-source build). If
you installed via Homebrew, `concept2upload` is already on your `PATH` —
drop the `./dist/` prefix.

Save your token once — it's cached in your OS's standard config directory
(e.g. `~/Library/Application Support/concept2upload/concept2.token` on
macOS, `~/.config/concept2upload/concept2.token` on Linux; owner-only
permissions) so you don't have to pass `--token` or set `CONCEPT2_TOKEN`
again:

```bash
./dist/concept2upload auth-concept2 your-access-token
```

`--token`/`CONCEPT2_TOKEN` still work and take priority when set (and are
themselves cached to that file for next time), so a one-off
`--token ...` on any command also works without running `auth-concept2`
first.

List your 10 most recent workouts, numbered 1 (most recent) upward:

```bash
./dist/concept2upload list
```

```
#   Date             Type     Distance        Time  Workout
1   2026-09-18 12:31 bike      13079m     30:00.0  VariableInterval
2   2026-09-07 06:56 bike        742m      2:23.8  JustRow
...
```

Show metadata for a single workout (by the position shown above) — date,
type, distance, duration, calories, drag factor, cadence/stroke rate, avg
power, heart rate, source, and segment count:

```bash
./dist/concept2upload show 1
```

```
Workout #1 (Concept2 id 123456789)
Date:                              2026-09-18 12:31:00 (Europe/Dublin)
Type:                              bike
Workout type:                      VariableInterval
Distance:                          13079 m
Duration:                          30:00.0
Calories:                          312 kcal
Drag factor:                       132
Avg cadence (rpm):                 87
Avg power:                         198 W
Heart rate:                        avg 142, max 167
Source:                            ErgData
Stroke-by-stroke data available:   yes
Segments:                          4 intervals
```

Download a single workout (by the position shown above) as a `.tcx` file
into `./workout/`:

```bash
./dist/concept2upload get 1
```

`get` only ever downloads one workout per invocation. It reuses the exact
result shown by your last `list` call when possible (via
`workout/.last_list.json`), so `get 3` really is the workout you saw at
position 3. It never overwrites an existing file — if the target name is
already taken, it appends `-1`, `-2`, etc.

Import the resulting `.tcx` file into Garmin Connect via
**Import Data** on the Garmin Connect website, or drag-and-drop it onto
Strava's **Upload Activity** page. Each Concept2 interval/split becomes its
own `<Lap>` (with per-lap average/max heart rate and cadence), and the file
carries a `<Notes>` summarizing the workout (type, distance, duration, avg
power/heart rate — since Concept2's API has no title field of its own),
plus your Concept2 comment if you left one, and a credit back to this
project.

### Uploading straight to Strava

1. Create a Strava API application at <https://www.strava.com/settings/api>
   and note its **Client ID** and **Client Secret**, shown near the top of
   the app's page. Ignore **"Your Access Token"/"Your Refresh Token"**
   further down that page — those are Strava's own quick-test tokens,
   scoped read-only, and can't upload activities. This tool runs its own
   OAuth flow (below) to get a token with the write access it actually
   needs.
2. Authorize once (opens your browser; you log in and grant access
   yourself — this tool never sees your Strava password), giving your
   Client ID/Secret either as flags:
   ```bash
   ./dist/concept2upload auth-strava --client-id=your-client-id --client-secret=your-client-secret
   ```
   or as environment variables:
   ```bash
   export STRAVA_CLIENT_ID=your-client-id
   export STRAVA_CLIENT_SECRET=your-client-secret
   ./dist/concept2upload auth-strava
   ```
   Either way, they're only needed the first time — they're then cached in
   the same OS config directory as your Concept2 token (owner-only
   permissions), so later `auth-strava`/`upload-strava` runs don't need
   them set again.
3. Upload a specific downloaded workout, by the position shown in `list`:
   ```bash
   ./dist/concept2upload upload-strava 1
   ```

## How watts are computed

Concept2's published power formula, `watts = 2.80 / (split_seconds / 500)^3`,
is applied to every machine type. "Split" is whatever Concept2 itself uses
as the pace value: time per 500m for RowErg/SkiErg/dynamic, time per 1000m
for BikeErg — the same `/500` divisor is used either way, since Concept2
doesn't rescale BikeErg's split before applying the formula (see the
[Concept2 watts calculator](https://www.concept2.com/training/watts-calculator)
and [Erg Arcade's pace derivatives writeup](https://ergarcade.com/articles/c2-pace-derivatives)).

Workouts with stroke-by-stroke data (`stroke_data: true` in the API) get a
per-stroke watts value. Workouts without it fall back to one trackpoint per
interval/split, with watts estimated from each segment's own average pace.

## Development

```bash
make fmt-check vet build test
```

## Web app (Firebase + Cloud Run)

The CLI's core logic (`internal/concept2`, `internal/tcx`) is also wrapped
as a small multi-user JSON API in `cmd/server`, meant to run on
**Cloud Run** behind **Firebase Hosting**, with **Firebase Auth** for
sign-in and **Firestore** replacing the CLI's local token file
(`internal/webstore`). A minimal static frontend lives in `web/`. It covers
browsing and downloading Concept2 workouts as `.tcx` files; uploading to
Strava stays a CLI-only feature (`concept2upload upload-strava`) - Strava
now gates registering an API application behind a paid plan, so the web
app doesn't run its own Strava OAuth flow.

This is a scaffold, not a hosted product - you deploy your own copy to your
own Firebase project.

### One-time setup

1. Create a Firebase project (<https://console.firebase.google.com>) and
   enable **Authentication > Google sign-in**, **Firestore**, and
   **Cloud Run**/**Cloud Build** (via the Firebase/GCP console - Cloud Run
   requires the Blaze, pay-as-you-go plan; Firestore/Hosting/Auth alone
   don't).
2. Fill in `web/firebase-config.js` with your project's web app config
   (Project settings > General > Your apps).
3. Install the [Firebase CLI](https://firebase.google.com/docs/cli) and the
   [gcloud CLI](https://cloud.google.com/sdk/docs/install), then
   `firebase login` / `gcloud auth login`.

### Deploy manually

```bash
# Build and deploy the API to Cloud Run.
gcloud run deploy concept2upload-server \
  --source . \
  --region us-central1 \
  --allow-unauthenticated

# Deploy Firestore rules and the frontend (with the Cloud Run rewrite from
# firebase.json).
firebase deploy --only firestore:rules,hosting
```

`--allow-unauthenticated` is safe here: every route still requires a valid
Firebase Auth ID token, checked inside `cmd/server` itself (see `withAuth`
in `cmd/server/main.go`). Cloud Run's own default service account already
has the credentials `cmd/server` needs for Firebase Auth/Firestore - no
service account key file to manage.

### Continuous deployment (GitHub Actions)

`.github/workflows/deploy-firebase.yml` runs the same two deploy steps
above automatically on every push to `main`, gated on `go build`/`vet`/
`test`/`gofmt` passing first. It needs these repo secrets
(Settings > Secrets and variables > Actions):

| Secret          | Value                                                                                                                                               |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GCP_SA_KEY`     | JSON key for a service account with the **Cloud Run Admin**, **Cloud Build Editor**, **Artifact Registry Writer**, **Service Account User**, and **Firebase Hosting Admin** roles on your project |
| `GCP_PROJECT_ID` | Your Firebase/GCP project ID                                                                                                                       |

A long-lived JSON key is the simplest way to get this running; swap it for
[Workload Identity Federation](https://github.com/google-github-actions/auth#setup)
(no key file at all) once the pipeline is working end to end.

### Local development

```bash
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/a/service-account-key.json  # for local Firestore/Auth access
go run ./cmd/server
```

Serve `web/` with any static file server (e.g. `npx serve web`) and set
`Access-Control-Allow-Origin`/CORS as needed - `cmd/server` already sends
permissive CORS headers for this.
