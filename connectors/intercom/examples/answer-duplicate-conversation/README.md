# Intercom answer-duplicate-conversation example

This example answers a new Intercom conversation that duplicates the
customer's earlier open conversation, then closes the new one. The
`conversationEvent` Trigger starts one `IntercomAnswerDuplicateConversation`
Flow per new conversation, and the Flow uses every Intercom operation:

1. the Worker mounts `NewLocalConversationEventEndpointRunner` at
   `/webhooks/intercom` and starts it at once, so a notification is recorded in
   the binding's durable inbox even while Dex is unreachable;
2. the `inbound-conversation` binding accepts the topics its
   `conversationTopicPicker` saved, and `AcceptInboundConversation` admits only
   `conversation.user.created`;
3. `sdkgo.NewDexFlowTriggerTarget` starts the Flow with ID
   `intercom-duplicate-conversation-<conversation ID>` and the notification ID
   as request ID, so a redelivery starts no second Flow;
4. `RecordInboundConversation` validates the conversation ID and the replying
   admin, and `ReadInboundConversation` calls `getConversation`;
5. `ChooseCustomerEmail` skips a conversation that is closed or snoozed, that
   an admin already answered, or that no contact started, and otherwise looks
   up the sender's email;
6. `FindCustomerContacts` calls `findContactByEmail`, which can return a lead
   and a user for the same person. `CompleteWithoutContact` leaves the
   conversation open when there is none yet;
7. `SearchEarlierConversations` calls `searchConversations` for those
   contacts' open and snoozed conversations, and `ChooseEarlierConversation`
   picks the oldest one created before the new conversation. Without one, the
   Flow completes as `leftOpen` and writes nothing;
8. `ReplyToDuplicate` calls `replyToConversation` once, as the admin chosen
   with the `adminPicker`, pointing the customer to the earlier conversation.
   `RecordDuplicateReply` records the reply, and `RecordReplyNeedsReview`
   completes as `needsReview` when the reply's outcome cannot be confirmed;
9. `CloseDuplicate` calls `updateConversationState` to close the new
   conversation as the same admin, and `CompleteAnsweredDuplicate` completes
   as `answeredAndClosed`.

Only the happy-path branches, `notFound` of `findContactByEmail`, and
`uncertain` of `replyToConversation` are wired. A missing conversation, a
rejected request, an invalid response, and a local defect fail the Flow.

## 1. Generate the Flow Definition

From `connectors/intercom`, generate strict FDG 2.0 with the latest stable
dexcli release
(`python3 script/dex_compatibility.py install-dexcli --output <path>` installs
it):

```bash
mkdir -p build
dexcli visualize ./examples/answer-duplicate-conversation/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/answer-duplicate-conversation
```

The command must report `valid: true` with the twelve Steps and the
`inbound-conversation` binding. Inside this repository it also warns
`connector_release_required` and `connector_trigger_release_required`, because
a local module is not a published release.

## 2. Start Dex with this connector's release metadata

The connector has a Studio bundle, so build it and its release metadata from
this source; without them the connection shows **Unsupported**. From the
repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
(npm ci --prefix sdk/react && npm run build --prefix sdk/react)
(cd connectors/intercom/ui && npm ci && npm run build)
mkdir -p /tmp/intercom-release
go run ./cmd/connectorctl ui-artifact --manifest connectors/intercom/connector.yaml \
  --ui-root connectors/intercom/ui/dist \
  --output /tmp/intercom-release/connector-ui.tgz --digest-output /tmp/intercom-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/intercom/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/intercom \
  --version v0.1.0 --tag connectors/intercom/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/intercom-release/connector-ui.tgz --ui-digest /tmp/intercom-release/connector-ui.tgz.sha256 \
  --output /tmp/intercom-release/connector-release.json \
  --digest-output /tmp/intercom-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/intercom/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override intercom=/tmp/intercom-release
```

## 3. Configure the connection, the admin, and the topics

Follow the [Intercom setup](../../README.md#intercom-setup), then open the Dex
Web URL that dexcli prints, select **Connectors**, and select **Intercom**
(connection `intercom-support-inbox`):

- `region`: the workspace's region;
- `access_token`: the app's access token;
- `client_secret`: the app's client secret, which verifies webhooks.

Save the connection. Dex Web then shows this Flow's two configuration units:

- **Replying admin** on the `ReplyToDuplicate` Step: choose **Load admins**,
  select the teammate whose name the customer sees, and save. The same admin
  closes the duplicate. Dex Web saves the choice beside the connection file:

```json
{
  "connectorId": "intercom",
  "connectionName": "intercom-support-inbox",
  "operationId": "replyToConversation",
  "flowType": "IntercomAnswerDuplicateConversation",
  "stepType": "ReplyToDuplicate",
  "configuration": {"adminId": "5017691"}
}
```

- **Topics that start the Flow** on the `inbound-conversation` binding: select
  `conversation.user.created` and save. Dex Web stores the binding beside the
  connection:

```json
{
  "connectorId": "intercom",
  "connectionName": "intercom-support-inbox",
  "triggerName": "conversationEvent",
  "bindingName": "inbound-conversation",
  "configuration": {"topics": ["conversation.user.created"]}
}
```

## 4. Run the Worker and receive a new conversation

In a second terminal, from `connectors/intercom`:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/intercom"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/answer-duplicate-conversation
```

The Worker listens on `127.0.0.1:8851` and the webhook endpoint on
`127.0.0.1:8852`; override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, `INTERCOM_WEBHOOK_BIND_ADDRESS`, or
`DEX_BLOB_CACHE_DIR` when needed, and set `LOG_LEVEL=debug` to see every
delivery. `GET /readyz` answers `200` once the binding receives notifications.
For local verification against an Intercom-compatible fake only,
`INTERCOM_LOCAL_API_BASE_URL` replaces the regional API host; production
Workers leave it unset.

Expose `http://127.0.0.1:8852/webhooks/intercom` through an HTTPS tunnel, then
in the Intercom app open **Configure > Webhooks**, enter the public URL
(Intercom validates it with a HEAD request), subscribe to
`conversation.user.created`, and save. When a customer who already has an open
conversation starts another one, Dex Web shows the completed run
`intercom-duplicate-conversation-<conversation ID>` with this
`intercom-duplicate-conversation-outcome`:

```json
{
  "action": "answeredAndClosed",
  "conversationId": "215472658213",
  "earlierConversationId": "215472658001",
  "replyPartId": "34015896",
  "conversationState": "closed"
}
```

A customer without an earlier open conversation completes as `leftOpen` with
reason `noEarlierConversation`, and nothing is written to Intercom. The Flow can
also be started from Dex Web **Start Flow** with
`{"conversationId": "215472658213"}`.

## 5. Redeliveries, forgeries, and other topics

- Intercom redelivers a notification it did not see acknowledged within five
  seconds. The endpoint answers `200`, and no second Flow starts: the
  notification ID is the Flow-start request ID.
- A notification whose `X-Hub-Signature` does not match is answered `400` and
  nothing is recorded.
- Intercom's periodic `ping`, and topics the binding does not accept, are
  answered `200` and start nothing.

## 6. Restart recovery

Stop Dex, let Intercom deliver a notification, and stop the Worker. The
endpoint answered `200` because the notification was on disk; it stays in the
binding's inbox, a `.trigger-inbox-*.json` file beside the connection file.
Start Dex and the Worker again: the runner replays it, logs `replaying pending
trigger events`, and the Flow starts and completes.

## Test

From `connectors/intercom`:

```bash
go test -race ./examples/answer-duplicate-conversation/...
```

The unit tests cover the Flow's identities, admission rule, skip and choice
rules, mappers, the configuration loading, and the README samples.

With the latest Dex development server running, the real-Dex tests drive the
Flow on a real Worker against a stateful fake Intercom that requires the
bearer token and `Intercom-Version: 2.16`:

- a duplicate is answered once and closed, with the expected contact search,
  conversation search, reply, and close requests, while the earlier
  conversation, the customer's closed conversation, and another customer's
  conversation are untouched;
- a conversation without an earlier one is left open, and an answered one is
  skipped before any lookup;
- a reply held past Dex's async local phase is sent once (the duplicate-dispatch
  test);
- a reply whose response was lost is found in the conversation and never
  resent;
- a reply answered 503 without being applied completes as `needsReview` and is
  never resent;
- a Worker lost while Intercom holds the reply leaves the next attempt to read
  the conversation instead of resending;
- a close held past the local phase is dispatched again and converges on
  `closed`, whether Intercom accepts or rejects the redundant close;
- a rate-limited search waits for `X-RateLimit-Reset`;
- a rejected reply fails the Flow without Intercom's message text;
- an unconfigured admin fails the Flow before any Intercom request;
- the example's `run` function answers HEAD, starts one Flow per signed new
  conversation, deduplicates a redelivery, rejects a forgery, ignores a ping
  and another topic, and replays a notification acknowledged while Dex was
  down.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/answer-duplicate-conversation/... -count=1 -v
```
