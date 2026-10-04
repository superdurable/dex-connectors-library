# Webhook form-submission example

This example receives signed form submissions and starts one
`WebhookFormSubmission` Flow per submission:

1. the Worker mounts `NewProjectRequestReceivedEndpointRunner` at
   `/webhooks/form-submission` and starts it at once, so a submission is
   recorded in the binding's durable project inbox even while Dex is
   unreachable;
2. the `form-submission-received` binding accepts only bodies whose
   `/event_type` is `form_response`; `AcceptSubmission` also requires a JSON
   object or form fields;
3. `sdkgo.NewDexFlowTriggerTarget` starts the Flow with ID
   `webhook-form-submission-<event ID>` and the event ID as request ID, so a
   redelivered submission starts no second Flow;
4. `RecordSubmission` stores the submission in the `webhook-form-submission`
   Attribute;
5. `ForwardSubmission` calls `sendEvent`, which POSTs a signed
   `form.submitted` event to the connection's `deliveryUrl`;
6. `RecordForwarded` stores the `delivered` outcome in the
   `webhook-form-forwarding` Attribute and completes the Flow, and
   `RecordForwardingFailure` stores a `rejected`, `uncertain`, or `defect`
   outcome with its safe message and fails the Flow.

The connection is configured for Typeform-style signatures: HMAC-SHA256 of the
body, base64-encoded after `sha256=` in `Typeform-Signature`, with the event ID
at `/event_id`. Any other [sender setup](../../README.md#sender-setup) works the
same way.

## 1. Generate the Flow Definition

From `connectors/superdurable/webhook`, generate strict FDG 2.0 with the latest
stable dexcli release
(`python3 script/dex_compatibility.py install-dexcli --output <path>` installs
it):

```bash
mkdir -p build
dexcli visualize ./examples/form-submission/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/form-submission
```

The command must report `valid: true` with the four Steps, all four
`sendEvent` branches routed, and the `form-submission-received` binding.
Inside this repository it also warns `connector_release_required` and
`connector_trigger_release_required`, because a local module is not a
published release, and `v2_start_input`, because the Trigger, not Dex Web
**Start Flow**, supplies the start input.

## 2. Start Dex with this connector's release metadata

The connector is part of this module, so Dex needs release metadata built from
this source; without it the connection shows **Unsupported**. From the
repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/webhook-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/superdurable/webhook/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook \
  --version v0.21.0 --tag connectors/superdurable/webhook/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/webhook-release/connector-release.json \
  --digest-output /tmp/webhook-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/superdurable/webhook/build" \
  --connector-release-override webhook=/tmp/webhook-release
```

## 3. Configure the connection

Open the Dex Web URL that dexcli prints, select **Connectors**, and select
**Webhook** (connection `webhook-form`). In the host-owned form:

- `signing_secret`: the secret typed into the sender. For a local test, use
  the output of `openssl rand -hex 32` and export it as
  `WEBHOOK_SIGNING_SECRET` in the terminal that sends requests;
- `verification`: `hmacSha256`;
- `signatureHeader`: `Typeform-Signature`; `signaturePrefix`: `sha256=`;
  `signatureEncoding`: `base64`;
- `eventIdPointer`: `/event_id`;
- `deliveryUrl`: the HTTPS URL of the service that receives forwarded
  submissions, such as an HTTPS request inspector you control. Leave it blank
  to see `ForwardSubmission` select `defect` and fail the Flow with guidance;
- leave the other fields at their defaults, and save.

Dex Web lists the `requestReceived` binding `form-submission-received` but has
no form for its filter, because the connector declares no Studio units. The
binding's record in the `triggerBindings` array of the project configuration,
beside the saved connection, carries the filter:

```json
{
  "connectorId": "webhook",
  "connectionName": "webhook-form",
  "triggerName": "requestReceived",
  "bindingName": "form-submission-received",
  "configuration": {"matchPointer": "/event_type", "matchValues": ["form_response"]}
}
```

## 4. Run the Worker and send a submission

Dex Web or Superverse Studio writes the connection and the binding to the
project configuration. In a second terminal, from
`connectors/superdurable/webhook`, run the Worker with the `DEX_PROJECT_*`
environment that names that configuration, as
[project configuration loading](../../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/superdurable/webhook"
go run ./examples/form-submission
```

The Worker listens on `127.0.0.1:8841` and the webhook endpoint on
`127.0.0.1:8842`; override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, `WEBHOOK_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when
needed, and set `LOG_LEVEL=debug` to see every delivery. `GET /readyz` answers
`200` once the binding receives submissions. To receive from a real sender,
expose `http://127.0.0.1:8842/webhooks/form-submission` through an HTTPS tunnel
and enter that public URL in the sender.

Send one signed submission:

```bash
body='{"event_id":"01J9ZQ3K7Y","event_type":"form_response","form_response":{"form_id":"contact","answers":[{"type":"email","email":"ada@example.com"}]}}'
signature=$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$WEBHOOK_SIGNING_SECRET" -binary | base64)
curl -i http://127.0.0.1:8842/webhooks/form-submission \
  -H 'Content-Type: application/json' \
  -H "Typeform-Signature: sha256=$signature" \
  --data-binary "$body"
```

The endpoint answers `200`, and Dex Web shows the completed run
`webhook-form-submission-01J9ZQ3K7Y` with this `webhook-form-forwarding`
outcome, whose `webhookId` the receiver saw in the `webhook-id` header:

```json
{"branch": "delivered", "webhookId": "msg_8d3f2c1a9b7e4d6f8a0b1c2d3e4f5a6b", "statusCode": 200}
```

## 5. Redeliver and forge

- Run the same `curl` again. It answers `200`, and no second Flow starts:
  the Flow ID is already taken, so the start is reported as a duplicate.
- Change the body after signing, for example replace `ada@` with `eve@`. The
  endpoint answers `400` and nothing is recorded.
- Send `"event_type":"form_response_partial"`. The binding filters it, the
  endpoint still answers `200`, and no Flow starts.

## 6. Submissions before the runner is up

Until the binding replays its inbox and starts receiving, the endpoint answers
`503`, which senders retry. `/readyz` reports the same state.

## 7. Restart recovery

Stop Dex, send a submission, and stop the Worker. The endpoint answered `200`
because the submission was stored; it stays in the binding's durable inbox in
project storage. Start Dex and the Worker again: the runner replays it, logs
`replaying pending trigger events`, and the Flow starts and completes.

## Test

From `connectors/superdurable/webhook`:

```bash
go test -race ./examples/form-submission/...
```

The unit tests cover the Flow's mapping and admission rules, and serve the
example's Flow target on the connection's endpoint against an unreachable Dex
Server: a signed submission is answered `200` and retried toward Dex, a forged
one `400`, and a filtered one `200` without reaching the target.

Opening a connection from a loaded project needs project storage, so the tests
build the README's connection with a static credential and run the binding
without its durable project inbox; the Connector SDK's own tests cover the
inbox, including replay after a restart. The real Dex tests serve the
example's Flow target against `dexcli dev` and a TLS receiver that verifies
each forwarded signature: a signed submission starts exactly one Flow; a
redelivery starts no second Flow and forwards nothing again; a forged
submission answers `400` and starts nothing; and a submission before the
binding runs answers `503` and the sender's retry starts the Flow:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/form-submission/... -count=1 -v
```
