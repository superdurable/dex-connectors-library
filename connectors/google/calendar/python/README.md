# Google Calendar Python connector

Install `dex-connector-google-calendar==0.1.0` alongside a compatible Dex Python SDK. The package exposes `Client`, generated `Config` and `Credentials`, a generated project connection with Google OAuth and Workspace delegated refresh, and operation-specific Step factories for `listEvents`, `getEvent`, `createEvent`, `updateEvent`, and `queryFreeBusy`. It has no Triggers.

See [`examples/calendar_events`](examples/calendar_events/README.md) for a runnable Flow with the static connection name `google-calendar-events-reader`. The example lists a bounded event window and persists the public result. It uses the generated project connection, so the Worker reads credentials from Dex project storage and never takes a token from Flow input.

Run provider replay and the generated OAuth agreement cases with `uv run pytest tests`. The example integration test requires a running `dexcli dev` and `DEX_FLOW_SERVICE_ADDRESS`, and can be run with `uv run pytest examples/calendar_events/integration_test.py`.

Go-recorded provider fixtures and a real Dex stack with a local Google stand-in verify request and Flow behavior. Live Google consent, delegated token minting against Google, and Calendar API calls remain unverified.
