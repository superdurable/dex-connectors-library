# Linear issue-events example

This example serves the Linear connector's webhook endpoint and starts one Flow per newly created issue:

1. `linear.NewProjectIssueEventReceivedEndpointRunner` verifies each delivery's `Linear-Signature` and signed
   `webhookTimestamp`, and records each Issue event that the `issue-created` binding accepts in its durable
   project inbox before answering 200;
2. the application's `AcceptIssueCreated` filter admits only `create` events of issues outside the trash, and
   `ResolveFlowID` names the Flow after the event ID, so a redelivery starts no second Flow;
3. `RecordIssueEvent` records the event, `ReadEventIssue` calls `linear.NewGetIssueStep` by the issue UUID, and
   `RecordIssueRead` completes with the issue; every other branch goes to `RecordReadFailure`, which records
   the safe reason and fails the Flow.

## 1. Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/linear` with the latest stable dexcli release:

```bash
dexcli visualize ./examples/issue-events/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/linear-issue-events
```

The command must report `valid: true` and list the `issue-created` binding under `connectorTriggerBindings`.
Inside this repository it also warns that the connector Step and the Trigger binding require a published
release.

## 2. Start Dex with this connector's release metadata

Build the Studio bundle and release metadata and start Dex with the override, exactly as in the
[issue-request example](../issue-request/README.md#configure-and-run).

## 3. Configure the connection and the binding

Follow the [Linear setup](../../README.md#linear-setup), open **Connections** in Dex Web, select
`linear / linear-workspace`, choose **Personal API key** or **Linear OAuth**, and save. Fill
`webhook_signing_secret` with the signing secret of the webhook from step 4; until it is saved, the endpoint
answers 503 and Linear retries.

Then configure the `issue-created` binding of `issueEventReceived` for `LinearIssueEventRecorder`. Its **Team**
unit stores the team whose new issues start a Flow; leave it empty for every team. The project configuration
then holds:

```json
{
  "connectorId": "linear",
  "connectionName": "linear-workspace",
  "triggerName": "issueEventReceived",
  "bindingName": "issue-created",
  "configuration": {"teamId": "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"}
}
```

## 4. Create the Linear webhook

Expose the application's webhook listener through an HTTPS tunnel. As a workspace admin, open Linear
**Settings > API > Webhooks > New webhook**, enter the tunnel URL followed by `/webhooks/linear`, select the
team or all public teams and the **Issues** data change event, and save. Copy the webhook's signing secret into
`webhook_signing_secret` and save the connection again.

## 5. Run the Worker and send a delivery

From `connectors/linear`, start the application. It reads the `DEX_PROJECT_*` project configuration
environment described in [project configuration](../../../../sdkgo/projectconfig/README.md).

```bash
go run ./examples/issue-events
```

The webhook listens on `127.0.0.1:8838` at `/webhooks/linear`, `/readyz` answers 200 once the binding receives
events, and the Worker binds `127.0.0.1:8837`. Override `WEBHOOK_BIND_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`,
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL` when needed. `LINEAR_LOCAL_API_URL` points
`getIssue` at a local Linear-compatible fake for verification only.

Create an issue in the team; Linear sends the webhook. To send a delivery by hand instead, sign the body with
the secret saved in `webhook_signing_secret`; `webhookTimestamp` must be within a minute of now:

```bash
secret='<the webhook signing secret>'
now="$(($(date +%s) * 1000))"
body='{"action":"create","type":"Issue","createdAt":"2026-10-04T11:59:59.000Z","organizationId":"d4e5f6a7-b8c9-4d0e-9f1a-2b3c4d5e6f7a","webhookId":"e5f6a7b8-c9d0-4e1f-8a2b-3c4d5e6f7a8b","webhookTimestamp":NOW,"url":"https://linear.app/acme/issue/ENG-7","data":{"id":"9e5f6a7b-8c9d-4e0f-8a1b-3c4d5e6f7a8b","identifier":"ENG-7","number":7,"title":"Fire panel wiring","url":"https://linear.app/acme/issue/ENG-7","priority":0,"labelIds":[],"teamId":"2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4","stateId":"22222222-2222-4222-8222-222222222222","state":{"id":"22222222-2222-4222-8222-222222222222","name":"Todo","type":"unstarted"},"createdAt":"2026-10-04T11:59:58.000Z","updatedAt":"2026-10-04T11:59:58.000Z"}}'
body="${body/NOW/$now}"
signature="$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$secret" -hex | sed 's/^.* //')"
curl -i -X POST http://127.0.0.1:8838/webhooks/linear -H 'Content-Type: application/json' \
  -H "Linear-Signature: $signature" --data "$body"
```

The event ID is `create:9e5f6a7b-8c9d-4e0f-8a1b-3c4d5e6f7a8b:1791115198000`, so the Flow ID is
`linear-issue-create-9e5f6a7b-8c9d-4e0f-8a1b-3c4d5e6f7a8b-1791115198000`. With a real workspace the issue must
exist for `getIssue` to find it; otherwise the Flow records `notFound` and fails.

## 6. Redeliver and forge

Send the same body again with a new `webhookTimestamp` and signature, as Linear does on a retry: it answers
200, and the log records `trigger event delivered` with `duplicate=true` and no second Flow. A changed body, a
wrong secret, or a `webhookTimestamp` more than a minute away answers 400 and starts nothing.

## 7. Restart recovery

An event recorded while Dex is unreachable stays in the binding's durable inbox; after a restart, the endpoint
answers 503 until `Run` has replayed it, and the replay starts the Flow.

## Test

```bash
GOWORK=off go test -race ./examples/issue-events/...
```

Without Dex, the endpoint verifies, filters, and acknowledges deliveries and retries the accepted one toward
Dex. With the latest Dex development server running, the integration tests deliver signed webhooks to a real
Worker: a create starts one Flow that reads the issue back; Linear's re-signed retry is a duplicate start; a
stale `webhookTimestamp`, a wrong secret, and a tampered body answer 400; another team's issue and an update
start nothing; a delivery before the binding runs answers 503 and its retry starts the Flow.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/issue-events/... -count=1 -v
```
