# Calendly Python connector

Calendly scheduled events, invitees, cancellation, scheduling links, webhook subscriptions, and signed invitee webhooks for Dex Flows.

```bash
pip install dex-connector-calendly==0.2.0
```

The package implements six operations: `listScheduledEvents`, `getScheduledEvent`, `listEventInvitees`, `cancelScheduledEvent`, `createSchedulingLink`, and `createWebhookSubscription`. OAuth and personal access token decoding, refresh, and project connection wiring are manifest-generated. From 0.2.0 it also implements the `inviteeEventReceived` Trigger at parity with Go, and it requires `dex-connectors-sdk` 0.12.0 or newer.

See [the scheduled event example](examples/scheduled_event/README.md) for the static `calendly-scheduling` connection and Worker entrypoint. The connector source defines every operation's request and result type; the generated Step factories expose the stable branches. The `createSchedulingLink` Mutation uses Dex's single-dispatch fence and returns `uncertain` when a possibly accepted write cannot be confirmed.

## inviteeEventReceived Trigger

Each connection has one shared webhook endpoint for the HTTPS callback URL you register with `createWebhookSubscription`. It behaves as the Go connector's endpoint:

- It verifies `Calendly-Webhook-Signature`: `t=` must be within the connection's `webhookSignatureTolerance` of the receiving clock, either way, and one `v1=` must equal the hex HMAC-SHA256 of `t.body` under the connection's `webhook_signing_key`, compared in constant time. A failure answers `400` and never says which check failed.
- A blank signing key, or credentials that cannot be loaded or refreshed, answer `503`, so Calendly keeps retrying until the key is saved. A body over `webhookMaxBodyBytes` answers `413`, and a method other than POST answers `405`.
- `invitee.created` and `invitee.canceled` decode to `InviteeEvent` with the invitee and its scheduled event. The Trigger event ID is the webhook event followed by the scheduled event and invitee IDs, such as `invitee.created:EVENT0001:INVITEE01`, so a redelivery keeps its ID. Another webhook event, such as `routing_form_submission.created`, is acknowledged with `200` and no record.
- Each binding stores `events` (`invitee.created`, `invitee.canceled`, or both when empty) and `eventTypeUri`, which the `eventTypePicker` unit writes; blank accepts every event type. A filtered event is still acknowledged.
- Until every binding runs, verified deliveries answer `503`.

`new_project_invitee_event_received_endpoint_runner` loads the connection and every route's stored binding and wraps each target in the binding's durable project inbox, so an event is recorded before Calendly receives `200` and replays after a restart. The returned `InviteeEventReceivedEndpointRunner` is an ASGI 3 application whose `lifespan` runs the bindings; a host that owns the event loop calls `run(context)` instead, and `running_source_count()` serves a readiness probe. Stored `InviteeEvent` payloads use the Go connector's JSON field names, so Go and Python replicas share one inbox. `new_invitee_event_received_trigger` and `invitee_event_received_webhook_handler` build one binding and the endpoint directly from a provider connection; a mock connection has no signing key and is refused.

The [invitee recorder example](examples/invitee_recorder/README.md) wires the runner to a Flow that starts once per booked invitee and reads the scheduled event back:

```python
async def open_invitee_receiver(
    project: LoadedProject,
    dex_client: dex.AsyncClient,
    flow: InviteeRecorderFlow,
    http_client: httpx.AsyncClient | None = None,
) -> InviteeEventReceivedEndpointRunner:
    """Route the stored binding to the Flow through its durable inbox; close it with ``aclose``."""
    route = ProjectInviteeEventReceivedTriggerRoute(BINDING_NAME, new_booking_target(dex_client, flow))
    return await new_project_invitee_event_received_endpoint_runner(
        project, CONNECTION_NAME, [route], http_client
    )
```

Python `datetime` holds microseconds, so a webhook timestamp with nonzero digits beyond the sixth fractional digit is refused with `400`, where Go keeps nanoseconds; Calendly sends microseconds. Live Calendly webhooks have not been verified for the Python package.

## Test

```bash
uv sync --locked
uv run pytest
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/scheduled_event/integration_test.py examples/invitee_recorder/integration_test.py
```

`tests/test_invitee_event_received_trigger.py` replays a recording of the Go connector's decoder (`tests/fixtures/invitee_event_received_go_recording.json`) and checks the outcome, event ID, time, and stored payload of every body.
