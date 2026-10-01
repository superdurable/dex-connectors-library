# Help Scout conversation-triage example

This example receives signed Help Scout webhooks and starts one
`HelpScoutConversationTriage` Flow per new conversation:

1. the Worker mounts `NewLocalConversationEventEndpointRunner` at
   `/webhooks/helpscout` and starts it at once, so a delivery is recorded in the
   binding's durable inbox even while Dex is unreachable;
2. the `new-conversations` binding of the `conversationEvent` Trigger keeps
   only conversations of the inbox chosen with the `mailboxPicker` unit, and
   `AcceptNewConversation` admits only a `convo.created` event for an `active`
   conversation;
3. `sdkgo.NewDexFlowTriggerTarget` starts the Flow with ID
   `helpscout-convo.created-<conversation id>-<body digest>` and the Trigger
   event ID as request ID, so a redelivered webhook starts no second Flow;
4. `RecordTriageRequest` stores the event in the `helpscout-triage-request`
   Attribute;
5. `ReadConversation` calls `getConversation` with the five newest threads, and
   `InspectConversation` records the status and the primary customer's email;
6. `FindCustomerProfiles` calls `findCustomerByEmail`, and
   `RecordCustomerProfiles` records every matching profile, or none;
7. `SearchActiveConversations` calls `searchConversations` for the customer's
   `active` conversations in the inbox, and `RecordActiveConversations`
   records the others and writes the note;
8. `AddTriageNote` calls `replyToConversation` with `isInternalNote`, so the
   customer is never emailed, and `RecordTriageNote` records its thread ID;
9. `TagTriagedConversation` calls `updateConversation` to add `dex-triaged`,
   and `dex-repeat-contact` when another active conversation exists;
10. `CompleteTriage` stores the tags in the `helpscout-triage` Attribute and
    completes the Flow. Every other branch of every Help Scout Step goes to a
    `Record…Failure` Step that stores the branch and its safe message and fails
    the Flow; an `uncertain` note is never sent again.

## 1. Generate the Flow Definition

From `connectors/helpscout`, generate strict FDG 2.0 with the dexcli release
pinned in the repository's `.dex-compat-version` file
(`python3 script/dex_compatibility.py install-dexcli --output <path>` installs
it):

```bash
mkdir -p build
dexcli visualize ./examples/conversation-triage/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/conversation-triage
```

The command must report `valid: true` with the 16 Steps, every branch of the
five Help Scout Steps routed, and the `new-conversations` binding with its
`mailboxPicker` unit. Inside this repository it also warns
`connector_release_required` and `connector_trigger_release_required`, because
a local module is not a published release.

## 2. Start Dex with this connector's release metadata

The connector is part of this module, so Dex needs release metadata and the
Studio bundle built from this source; without them the connection shows
**Unsupported**. From the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
npm ci --prefix sdk/react && npm run build --prefix sdk/react
npm ci --prefix connectors/helpscout/ui && npm run build --prefix connectors/helpscout/ui
mkdir -p /tmp/helpscout-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/helpscout/connector.yaml \
  --ui-root connectors/helpscout/ui/dist \
  --output /tmp/helpscout-release/connector-ui.tgz \
  --digest-output /tmp/helpscout-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/helpscout/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/helpscout \
  --version v0.1.0 --tag connectors/helpscout/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/helpscout-release/connector-ui.tgz \
  --ui-digest /tmp/helpscout-release/connector-ui.tgz.sha256 \
  --output /tmp/helpscout-release/connector-release.json \
  --digest-output /tmp/helpscout-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/helpscout/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override helpscout=/tmp/helpscout-release
```

## 3. Configure the connection

Open the Dex Web URL that dexcli prints, select **Connectors**, and select
**Help Scout** (connection `helpscout-support`). Follow the guide shown above
the form: create the app under **Your Profile > My Apps** at
<https://secure.helpscout.net/> with any HTTPS placeholder as its Redirection
URL, enter its App ID and App Secret, enter a `webhook_secret` such as the
output of `openssl rand -hex 20`, leave `access_token` blank, and save. Export
the same webhook secret as `HELPSCOUT_WEBHOOK_SECRET` in the terminal that
sends test deliveries. Dex Web saves these credentials:

```json
{"app_id": "<App ID>", "app_secret": "<App Secret>", "webhook_secret": "<openssl rand -hex 20>"}
```

On its first Help Scout call the application obtains a two-day access token
with the client credentials grant and adds `access_token` and
`credentialExpiresAt` to the same record; nothing is edited by hand.

Then open the Flow's `new-conversations` binding and, in the **Inbox** unit,
choose **Load inboxes**. The list uses the stored access token, so it works
once the application has run; before that, enter the inbox ID in the
**Inbox ID fallback** field. Select the inbox whose new conversations start the
Flow and save. The saved binding configuration looks like this; no
`mailboxId` triages every inbox:

```json
{
  "connectorId": "helpscout",
  "connectionName": "helpscout-support",
  "triggerName": "conversationEvent",
  "bindingName": "new-conversations",
  "configuration": {"mailboxId": 123}
}
```

## 4. Create the Help Scout webhook

Expose `http://127.0.0.1:8872/webhooks/helpscout` through an HTTPS tunnel. In
Help Scout open the gear icon (Settings) > **Workspace > Apps** and select the
**Webhooks** app; enter the public URL, the same secret as the webhook's secret
key, and the conversation-created event. Or make the call with an access token
from the same client credentials exchange:

```bash
HELPSCOUT_TOKEN=$(curl -s https://api.helpscout.net/v2/oauth2/token \
  --data grant_type=client_credentials --data "client_id=$HELPSCOUT_APP_ID" --data "client_secret=$HELPSCOUT_APP_SECRET" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
curl -s https://api.helpscout.net/v2/webhooks \
  -H "Authorization: Bearer $HELPSCOUT_TOKEN" -H 'Content-Type: application/json' \
  --data "{\"url\":\"https://<tunnel>/webhooks/helpscout\",\"events\":[\"convo.created\"],\"secret\":\"$HELPSCOUT_WEBHOOK_SECRET\",\"mailboxIds\":[123],\"payloadVersion\":\"V3\",\"label\":\"Dex conversation triage\"}"
```

Leave Help Scout's `notification` flag off: its body names only a URL, which
the endpoint answers `400`.

## 5. Run the Worker and send a delivery

In a second terminal, from `connectors/helpscout`:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/helpscout"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/conversation-triage
```

The Worker listens on `127.0.0.1:8871` and the webhook endpoint on
`127.0.0.1:8872`; override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, `WEBHOOK_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when
needed, and set `LOG_LEVEL=debug` to see every delivery. `GET /readyz` answers
`200` once the binding receives deliveries.

Send an email to the inbox. To test without Help Scout, send one delivery
signed the way Help Scout signs it, the base64 HMAC-SHA1 of the body; the Flow's
reads then need a conversation your connection can read:

```bash
body='{"id":501,"number":12,"threads":1,"type":"email","status":"active","state":"published","subject":"Double charge on order 88213","mailboxId":123,"createdAt":"2026-09-30T11:58:00Z","primaryCustomer":{"id":238604,"type":"customer","first":"Jane","last":"Smith","email":"jane@acme.example.com"},"tags":[{"tag":"billing"}]}'
signature=$(printf '%s' "$body" | openssl dgst -sha1 -hmac "$HELPSCOUT_WEBHOOK_SECRET" -binary | base64)
curl -i http://127.0.0.1:8872/webhooks/helpscout \
  -H 'Content-Type: application/json' \
  -H 'X-HelpScout-Event: convo.created' \
  -H "X-HelpScout-Signature: $signature" \
  --data-binary "$body"
```

The endpoint answers `200`, and Dex Web shows the run
`helpscout-convo.created-501-<body digest>`. When every call succeeds, the
completed run's `helpscout-triage` Attribute holds the triage record:

```json
{"stage": "completed", "subject": "Double charge on order 88213", "status": "active", "newestThreadTypes": ["customer"], "customerEmail": "jane@acme.example.com", "customerProfileIds": [1001, 1002], "otherActiveConversationIds": [502], "noteThreadId": 5011, "tags": ["billing", "dex-triaged", "dex-repeat-contact"]}
```

The internal note reads
`Dex triage: 2 Help Scout customer profiles match jane@acme.example.com; the customer has 1 other active conversation in this inbox: 502.`

## 6. Redeliver and forge

- Send the same `curl` again. It answers `200`, and no second Flow starts: the
  body and so the event ID are unchanged, and the start is reported as a
  duplicate.
- Change the body after signing, for example replace `jane@` with `eve@`, or
  sign with another secret. The endpoint answers `400` and nothing is recorded.
- Send `X-HelpScout-Event: convo.tags`. It answers `200`;
  `AcceptNewConversation` consumes it without a Flow.
- Send a body whose `mailboxId` is another inbox. It answers `200`, and the
  binding records nothing.

## 7. Restart recovery

Stop Dex, send a delivery, and stop the Worker. The endpoint answered `200`
because the delivery was on disk; it stays in the binding's inbox, a
`.trigger-inbox-*.json` file beside the connection file. Start Dex and the
Worker again: the runner replays it, logs `replaying pending trigger events`,
and the Flow starts and completes.

## Test

From `connectors/helpscout`:

```bash
go test -race ./examples/conversation-triage/...
```

The unit tests cover the Flow's mapping, note, and admission rules, and run
the Worker against an unreachable Dex Server: a signed delivery is answered
`200` only after it is in the inbox, a tampered or wrongly signed one `400`,
and a delivery for another inbox `200` without a record.

The real Dex tests run the example's `run` function against `dexcli dev` and a
TLS stand-in for `api.helpscout.net`, starting from the credentials Dex Web
saves: a signed delivery starts exactly one Flow that obtains and stores one
access token, reads, looks up, searches, adds one internal note, and tags, and
a second Flow reuses the stored token; a redelivery
starts no second Flow and adds no second note; a forged delivery answers `400`
and starts nothing; a tags event is filtered; and a delivery acknowledged while
Dex was unreachable is replayed after a restart:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/conversation-triage/... -count=1 -v
```
