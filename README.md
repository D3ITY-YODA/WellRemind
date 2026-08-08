# WellRemind
A lightweight hydration and medication reminder app.

WellRemind is a browser-based reminder PWA that sends hydration and medication notifications using Web Push.

## Features

- Push notification reminders for water intake and medication.
- Browser service worker for background notification delivery.
- Persistent subscriber storage in a local JSON state file.
- Simple PWA manifest and icon assets.

## Run locally

```bash
go mod tidy
go run .
```

Then open `http://localhost:8080` in a Chromium-based browser.

## Health check

The server exposes a simple health endpoint at `/healthz`.

## Docker

Build the container:

```bash
docker build -t wellremind .
```

Run it:

```bash
docker run -p 8080:8080 wellremind
```

## Test

```bash
go test ./...
```

## Notes

- `wellremind-data.json` stores saved subscriptions and VAPID keys locally.
- This file is ignored by Git and is created automatically on first run.
