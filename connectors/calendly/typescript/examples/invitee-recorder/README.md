# Calendly invitee-recorder example

This TypeScript example mirrors the Go [invitee-recorder example](../../../go/examples/invitee-recorder/README.md). It receives signed Calendly webhooks and starts one `CalendlyInviteeRecorder` Flow per booked invitee:

1. `app.ts` mounts `newProjectInviteeEventReceivedEndpointRunner` at `/webhooks/calendly` and runs it at once, so a delivery is recorded in the binding's durable project inbox even while Dex is unreachable; the Worker starts once Dex answers, retrying with delays from 250 milliseconds to 30 seconds;
2. the `invitee-created` binding of the `inviteeEventReceived` Trigger keeps only bookings of the event type chosen with the `eventTypePicker` unit, and `acceptBooking` admits only an active `invitee.created` event;
3. `newDexFlowTriggerTarget` starts the Flow with ID `calendly-invitee.created-<event id>-<invitee id>` and the Trigger event ID as request ID, so a redelivered webhook starts no second Flow;
4. `RecordBooking` stores the booking in the `calendly-booking` Attribute;
5. `ReadScheduledEvent` calls `getScheduledEvent` with the invitee's scheduled event URI;
6. `RecordScheduledEvent` stores the `found` event in the `calendly-scheduled-event` Attribute and completes the Flow. Unwired optional branches fail the Flow with the connector's safe failure reason.

## Configure and run

Start `dexcli dev` and configure the `calendly-scheduling` connection in Dex Web as the Go example's README describes: a personal access token with the scopes `users:read`, `event_types:read`, `scheduled_events:write`, `scheduling_links:write`, and `webhooks:write`, and a `webhook_signing_key` such as the output of `openssl rand -hex 32`. Then open the Flow's `invitee-created` binding, choose **Load event types** in the **Event type** unit, and save; an empty `eventTypeUri` records bookings of every event type.

Run the Worker from `connectors/calendly/typescript` with the `DEX_PROJECT_*` project configuration environment that Dex Web writes:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run example:invitees
```

The webhook listens on `WEBHOOK_BIND_ADDRESS` (default `127.0.0.1:8852`), and `GET /readyz` answers `200` once the binding receives deliveries. Expose `/webhooks/calendly` through an HTTPS tunnel and register it once with `createWebhookSubscription` or the `curl` command in the Go example's README. Set `LOG_LEVEL=debug` to see every delivery. The signed `curl` delivery, the redelivery and forgery checks, and the restart recovery steps of the Go example's README apply unchanged.

## Test

`npm test` covers the Flow's mapping, admission rule, and binding declaration in [the workflow test](../../test/invitee_recorder_workflow.test.ts), and the endpoint's signature checks, filters, status codes, and durable inbox replay in [the Trigger test](../../test/trigger.test.ts).

The [integration test](../../test/integration/invitee-recorder.integration.test.ts) runs `app.ts` against a real Dex Server with a local stand-in for `api.calendly.com` and an in-memory project: a signed delivery starts exactly one Flow that reads the scheduled event once; a redelivery starts no second Flow and reads nothing again; a tampered delivery answers `400`; a cancellation is filtered; another event type is acknowledged without a record; a delivery stored while Dex is unreachable starts its Flow after a restart; and, without the project inbox, a delivery that arrives before the binding runs is answered `503`, and Calendly's retry starts the Flow.

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

Live Calendly webhooks and authorization have not been verified for this example.
