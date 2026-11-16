# Calendly invitee recorder example for Python

This Python example mirrors the Go [invitee-recorder example](../../../go/examples/invitee-recorder/README.md). It receives signed Calendly webhooks and starts one `CalendlyInviteeRecorderPython` Flow per booked invitee:

1. `app.py` serves `new_project_invitee_event_received_endpoint_runner` at `/webhooks/calendly`, so a delivery is recorded in the binding's durable project inbox before Calendly receives `200`; `GET /readyz` answers `200` once the binding receives deliveries;
2. the `invitee-created` binding of the `inviteeEventReceived` Trigger keeps only bookings of the event type chosen with the `eventTypePicker` unit, and `accept_booking` admits only an active `invitee.created` event;
3. `new_dex_flow_trigger_target` starts the Flow with ID `calendly-invitee.created-<event id>-<invitee id>` and the Trigger event ID as request ID, so a redelivered webhook starts no second Flow;
4. `RecordBooking` stores the booking in the `calendly-booking` Attribute;
5. `ReadScheduledEvent` calls `getScheduledEvent` with the invitee's scheduled event URI;
6. `RecordScheduledEvent` stores the `found` event in the `calendly-scheduled-event` Attribute and completes the Flow. Unwired optional branches fail the Flow with the connector's safe failure reason.

## Configure

Follow the [Calendly setup](../../../README.md) and the Go example's README: create the `calendly-scheduling` connection under **Connections** in Dex Web with a personal access token that has the scopes `users:read`, `event_types:read`, `scheduled_events:write`, `scheduling_links:write`, and `webhooks:write`, and a `webhook_signing_key` such as the output of `openssl rand -hex 32`. Then open the Flow's `invitee-created` binding, choose **Load event types** in the **Event type** unit, and save; an empty `eventTypeUri` records bookings of every event type. The receiver reads the saved values once at startup, so restart it after saving.

## Run

From `connectors/calendly/python`, generate strict FDG 2.0 and start `dexcli dev` with it:

```bash
mkdir -p build
uv run dexcli visualize ./examples/invitee_recorder/flow.py --schema-version 2.0 --json --out ./build/invitee_recorder
dexcli dev -flow-rendering-dir ./build
```

Set `DEX_CONNECTOR_CONFIG_FILE` to the `connections.json` that Dex Web wrote and run the Worker, which also serves the webhook. uvicorn is not a package dependency:

```bash
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run --with uvicorn python -m examples.invitee_recorder.app
```

The webhook listens on `WEBHOOK_BIND_ADDRESS` (default `127.0.0.1:8852`). Expose `/webhooks/calendly` through an HTTPS tunnel and register it once with `createWebhookSubscription` or the `curl` command in the Go example's README. Set `DEX_BLOB_CACHE_DIR` or `LOG_LEVEL` when needed; Trigger records, without payloads or credentials, go to the `dex_connectors.sdk.trigger` logger. The signed `curl` delivery, redelivery and forgery checks, and restart recovery steps of the Go example's README apply unchanged.

## Test

```bash
uv run pytest tests/test_invitee_recorder_example.py tests/test_invitee_event_received_trigger.py tests/test_invitee_event_received_project.py
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/invitee_recorder/integration_test.py
```

The unit tests cover the Flow's mapping, admission rule, binding declaration, and HTTP routing, and the endpoint's signature checks, filters, status codes, Go decoder parity, and durable inbox replay, including an event a Go replica recorded.

The integration test runs the receiver and Flow against a real Dex Worker and Client, with a local stand-in for `api.calendly.com` and the project file Dex Web writes: a booking recorded by a process that stopped before its Flow started replays after a restart; a signed booking starts exactly one Flow that reads its scheduled event once; a redelivery starts no second Flow and reads nothing again; a delivery signed with another key answers `400`; a cancellation, a booking of another event type, and a routing form event are acknowledged without a Flow; and the durable inbox ends empty.

Live Calendly webhooks and authorization have not been verified for this example.
