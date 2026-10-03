# Calendly invitee-recorder example

This example receives signed Calendly webhooks and starts one
`CalendlyInviteeRecorder` Flow per booked invitee:

1. the Worker mounts `NewLocalInviteeEventReceivedEndpointRunner` at
   `/webhooks/calendly` and starts it at once, so a delivery is recorded in the
   binding's durable inbox even while Dex is unreachable;
2. the `invitee-created` binding of the `inviteeEventReceived` Trigger keeps
   only bookings of the event type chosen with the `eventTypePicker` unit, and
   `AcceptBooking` admits only an active `invitee.created` event;
3. `sdkgo.NewDexFlowTriggerTarget` starts the Flow with ID
   `calendly-invitee.created-<event id>-<invitee id>` and the Trigger event ID as
   request ID, so a redelivered webhook starts no second Flow;
4. `RecordBooking` stores the booking in the `calendly-booking` Attribute;
5. `ReadScheduledEvent` calls `getScheduledEvent` with the invitee's scheduled
   event URI;
6. `RecordScheduledEvent` stores the `found` event in the
   `calendly-scheduled-event` Attribute and completes the Flow, and
   `RecordReadFailure` stores a `notFound`, `providerRejected`,
   `invalidResponse`, or `defect` outcome with its safe message and fails the
   Flow.

## 1. Generate the Flow Definition

From `connectors/calendly`, generate strict FDG 2.0 with the latest stable
dexcli release
(`python3 script/dex_compatibility.py install-dexcli --output <path>` installs
it):

```bash
mkdir -p build
dexcli visualize ./examples/invitee-recorder/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/invitee-recorder
```

The command must report `valid: true` with the four Steps, all five
`getScheduledEvent` branches routed, and the `invitee-created` binding with its
`eventTypePicker` unit. Inside this repository it also warns
`connector_release_required` and `connector_trigger_release_required`, because
a local module is not a published release.

## 2. Start Dex with this connector's release metadata

The connector is part of this module, so Dex needs release metadata and the
Studio bundle built from this source; without them the connection shows
**Unsupported**. From the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
npm ci --prefix sdk/react && npm run build --prefix sdk/react
npm ci --prefix connectors/calendly/ui && npm run build --prefix connectors/calendly/ui
mkdir -p /tmp/calendly-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/calendly/connector.yaml \
  --ui-root connectors/calendly/ui/dist \
  --output /tmp/calendly-release/connector-ui.tgz \
  --digest-output /tmp/calendly-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/calendly/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/calendly \
  --version v0.1.0 --tag connectors/calendly/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/calendly-release/connector-ui.tgz \
  --ui-digest /tmp/calendly-release/connector-ui.tgz.sha256 \
  --output /tmp/calendly-release/connector-release.json \
  --digest-output /tmp/calendly-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/calendly/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override calendly=/tmp/calendly-release
```

## 3. Configure the connection

Open the Dex Web URL that dexcli prints, select **Connectors**, and select
**Calendly** (connection `calendly-scheduling`). Choose **Personal access
token** and follow the guide shown above the form:

- `access_token`: a token from Calendly **Integrations > API & Webhooks**
  with the scopes `users:read`, `event_types:read`, `scheduled_events:write`,
  `scheduling_links:write`, and `webhooks:write`;
- `webhook_signing_key`: the output of `openssl rand -hex 32`. Export the same
  value as `CALENDLY_WEBHOOK_SIGNING_KEY` in the terminal that registers the
  subscription and sends test deliveries;
- leave the configuration fields at their defaults, and save.

Dex Web releases before `cli-v1.2.0` cannot save the personal access token
method of a connector with several sign-in methods (superdurable/dex#570, fixed
in `cli-v1.2.0`). With such a release, either run `dexcli` `cli-v1.2.0` or
later, or add the connection to the `connections` array of
`$HOME/.dex/connectors/connections.json` yourself, with
`"authMethodId": "personal-access-token"` beside the record's other members and
these credentials:

```json
{"auth_method": "personal-access-token", "access_token": "<personal access token>", "webhook_signing_key": "<openssl rand -hex 32>"}
```

Then open the Flow's `invitee-created` binding, choose **Load event types** in
the **Event type** unit, select the event type whose bookings start the Flow,
and save. The saved binding configuration looks like this; an empty
`eventTypeUri` records bookings of every event type:

```json
{
  "connectorId": "calendly",
  "connectionName": "calendly-scheduling",
  "triggerName": "inviteeEventReceived",
  "bindingName": "invitee-created",
  "configuration": {"eventTypeUri": "https://api.calendly.com/event_types/TYPE0001"}
}
```

## 4. Register the webhook subscription

Expose `http://127.0.0.1:8852/webhooks/calendly` through an HTTPS tunnel and
register its public URL once. A Flow can call `createWebhookSubscription`,
which reuses an existing subscription with the same URL; by hand it is one
call, with the organization and user URIs from `GET /users/me`:

```bash
curl -s https://api.calendly.com/users/me -H "Authorization: Bearer $CALENDLY_TOKEN"
curl -s https://api.calendly.com/webhook_subscriptions \
  -H "Authorization: Bearer $CALENDLY_TOKEN" -H 'Content-Type: application/json' \
  --data "{\"url\":\"https://<tunnel>/webhooks/calendly\",\"events\":[\"invitee.created\",\"invitee.canceled\"],\"organization\":\"<current_organization>\",\"user\":\"<uri>\",\"scope\":\"user\",\"signing_key\":\"$CALENDLY_WEBHOOK_SIGNING_KEY\"}"
```

## 5. Run the Worker and send a delivery

In a second terminal, from `connectors/calendly`:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/calendly"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/invitee-recorder
```

The Worker listens on `127.0.0.1:8851` and the webhook endpoint on
`127.0.0.1:8852`; override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, `WEBHOOK_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when
needed, and set `LOG_LEVEL=debug` to see every delivery. `GET /readyz` answers
`200` once the binding receives deliveries.

Book the event type on its Calendly scheduling page. To test without Calendly,
send one signed delivery the way Calendly signs it, `t=<unix seconds>,v1=<hex
HMAC-SHA256 of "t.body">`; the endpoint verifies it, and the Flow's
`getScheduledEvent` read then needs a scheduled event your token can read:

```bash
body='{"event":"invitee.created","created_at":"2026-09-30T11:59:00.000000Z","created_by":"https://api.calendly.com/users/USER0001","payload":{"uri":"https://api.calendly.com/scheduled_events/EVENT0001/invitees/INVITEE01","event":"https://api.calendly.com/scheduled_events/EVENT0001","email":"ada@example.com","name":"Ada Lovelace","status":"active","timezone":"Europe/London","questions_and_answers":[],"rescheduled":false,"old_invitee":null,"new_invitee":null,"scheduled_event":{"uri":"https://api.calendly.com/scheduled_events/EVENT0001","name":"30 Minute Meeting","status":"active","start_time":"2026-10-02T17:00:00.000000Z","end_time":"2026-10-02T17:30:00.000000Z","event_type":"https://api.calendly.com/event_types/TYPE0001","location":{"type":"physical","location":"Office"}}}}'
timestamp=$(date +%s)
signature=$(printf '%s.%s' "$timestamp" "$body" | openssl dgst -sha256 -hmac "$CALENDLY_WEBHOOK_SIGNING_KEY" -hex | sed 's/^.* //')
curl -i http://127.0.0.1:8852/webhooks/calendly \
  -H 'Content-Type: application/json' \
  -H "Calendly-Webhook-Signature: t=$timestamp,v1=$signature" \
  --data-binary "$body"
```

The endpoint answers `200`, and Dex Web shows the run
`calendly-invitee.created-EVENT0001-INVITEE01`. When the read succeeds, the
completed run's `calendly-scheduled-event` Attribute holds the event, with
every time in UTC:

```json
{"branch": "found", "scheduledEvent": {"uri": "https://api.calendly.com/scheduled_events/EVENT0001", "name": "30 Minute Meeting", "status": "active", "startTime": "2026-10-02T17:00:00Z", "endTime": "2026-10-02T17:30:00Z", "eventTypeUri": "https://api.calendly.com/event_types/TYPE0001", "location": {"type": "physical", "location": "Office"}, "inviteesCounter": {"total": 1, "active": 1, "limit": 1}}}
```

## 6. Redeliver and forge

- Send the same `curl` again with a fresh timestamp. It answers `200`, and no
  second Flow starts: the Flow ID is already taken, so the start is reported as
  a duplicate.
- Change the body after signing, for example replace `ada@` with `eve@`, or
  sign with another key. The endpoint answers `400` and nothing is recorded.
- Send `"event":"invitee.canceled"`. It answers `200`; `AcceptBooking`
  consumes it without a Flow.
- Reuse a timestamp older than three minutes. The endpoint answers `400`,
  which stops a replayed capture.

## 7. Restart recovery

Stop Dex, send a delivery, and stop the Worker. The endpoint answered `200`
because the delivery was on disk; it stays in the binding's inbox, a
`.trigger-inbox-*.json` file beside the connection file. Start Dex and the
Worker again: the runner replays it, logs `replaying pending trigger events`,
and the Flow starts and completes.

## Test

From `connectors/calendly`:

```bash
go test -race ./examples/invitee-recorder/...
```

The unit tests cover the Flow's mapping and admission rules, and run the
Worker against an unreachable Dex Server: a signed delivery is answered `200`
only after it is in the inbox, a tampered or wrongly signed one `400`, and a
delivery for another event type `200` without a record.

The real Dex tests run the example's `run` function against `dexcli dev` and a
TLS stand-in for `api.calendly.com`: a signed delivery starts exactly one Flow
that reads the scheduled event once; a redelivery starts no second Flow and
reads nothing again; a forged delivery answers `400` and starts nothing; a
cancellation is filtered; and a delivery acknowledged while Dex was unreachable
is replayed after a restart:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/invitee-recorder/... -count=1 -v
```
