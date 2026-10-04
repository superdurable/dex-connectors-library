# Front conversation-triage example

This example triages one Front conversation. Each `FrontConversationTriage`
Flow is started from Dex Web **Start Flow**, or by the application, with one
conversation ID, and it uses every Front operation:

1. `RecordTriageRequest` checks the conversation ID and that the triage tag is
   configured, and persists the request;
2. `ReadConversation` calls `getConversation` for the conversation with its
   five newest messages and 25 newest comments;
3. `InspectConversation` completes as `skipped` when a comment starting with
   `Dex triage:` shows that an earlier Flow triaged the conversation, and
   otherwise takes the requester's email: the `from` handle of the newest
   inbound message, or the conversation's main recipient;
4. `FindRequester` calls `findContactByEmail`; `RecordRequester` records the
   contact ID, or none on `notFound`;
5. `SearchOpenConversations` calls `searchConversations` for the requester's
   open conversations, in the inbox chosen with the `inboxPicker`, and
   `RecordOpenConversations` keeps every one on the first page but the
   triaged conversation and records whether Front has more;
6. `AddTriageComment` calls `replyToConversation` once with `isInternalNote`
   set, writing a comment only teammates see that names the contact and up to
   ten other open conversations, with "at least" before the count when Front
   has another search page. `RecordCommentNeedsReview` completes as
   `needsReview`, without routing, when the comment's outcome is unknown;
7. `AssignAndTagConversation` calls `updateConversation` to add the tag chosen
   with the `tagPicker` and assign the teammate chosen with the
   `teammatePicker`, and `CompleteTriage` completes with the triage record.

Only the happy-path branches, `notFound` of `findContactByEmail`, and
`uncertain` of `replyToConversation` are wired. A missing or merged
conversation, a rejected request, an invalid response, and a local defect
fail the Flow with the connector's safe message. The Flow never writes to the
customer.

## 1. Generate the Flow Definition

From `connectors/front`, generate strict FDG 2.0 with the latest stable dexcli
release (`python3 script/dex_compatibility.py install-dexcli --output <path>`
installs it):

```bash
mkdir -p build
dexcli visualize ./examples/conversation-triage/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/conversation-triage
```

The command must report `valid: true` with the twelve Steps. Inside this
repository it also warns `connector_release_required`, because a local module
is not a published release.

## 2. Start Dex with this connector's release metadata

The connector has a Studio bundle, so build it and its release metadata from
this source; without them the connection shows **Unsupported**. From the
repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
(npm ci --prefix sdk/react && npm run build --prefix sdk/react)
(cd connectors/front/ui && npm ci && npm run build)
mkdir -p /tmp/front-release
go run ./cmd/connectorctl ui-artifact --manifest connectors/front/connector.yaml \
  --ui-root connectors/front/ui/dist \
  --output /tmp/front-release/connector-ui.tgz --digest-output /tmp/front-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/front/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/front \
  --version v0.21.0 --tag connectors/front/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/front-release/connector-ui.tgz --ui-digest /tmp/front-release/connector-ui.tgz.sha256 \
  --output /tmp/front-release/connector-release.json \
  --digest-output /tmp/front-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/front/build" \
  --connector-release-override front=/tmp/front-release
```

## 3. Configure the connection and the pickers

Follow the [Front authorization](../../README.md#authorization), then open the
Dex Web URL that dexcli prints, select **Connectors**, and select **Front**
(connection `front-support`). Paste the API token into `api_token` and save.
Dex Web then shows this Flow's three configuration units:

- **Inbox to search** on the `SearchOpenConversations` Step: choose **Load
  inboxes**, select the inbox whose open conversations count, or leave it empty
  to search every inbox, and save;
- **Triage tag** and **Assignee** on the `AssignAndTagConversation` Step:
  choose **Load tags** and select the tag, which is required, then **Load
  teammates** and select the teammate, or leave it empty to keep the current
  assignee, and save each. Dex Web saves both in one configuration beside the
  connection:

```json
{
  "connectorId": "front",
  "connectionName": "front-support",
  "operationId": "updateConversation",
  "flowType": "FrontConversationTriage",
  "stepType": "AssignAndTagConversation",
  "configuration": {"assigneeId": "tea_2thf", "tagId": "tag_13o8r1"}
}
```

## 4. Run the Worker and triage a conversation

Dex Web or Superverse Studio writes the connection and the picks to the
project configuration. In a second terminal, from `connectors/front`, run the
Worker with the `DEX_PROJECT_*` environment that names that configuration, as
[project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/front"
go run ./examples/conversation-triage
```

The Worker listens on `127.0.0.1:8861`; override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when needed, and set
`LOG_LEVEL=debug` for more detail. The picks are read once at startup, so
restart the Worker after changing them. For local verification against a
Front-compatible fake only, `FRONT_LOCAL_API_BASE_URL` replaces
`https://api2.frontapp.com`; production Workers leave it unset.

In Dex Web choose **Start Flow** for `FrontConversationTriage` with a
conversation ID, such as one `searchConversations` or Front's
`GET https://api2.frontapp.com/conversations` returns. The number in Front's
own address bar is not that ID:

```json
{"conversationId": "cnv_55c8c149"}
```

A completed run returns a `front-triage` record like this one:

```json
{
  "stage": "completed",
  "subject": "Double charge on order 88213",
  "status": "assigned",
  "requesterEmail": "jane@acme.example.com",
  "contactId": "crd_1y8sp71",
  "otherOpenConversationIds": ["cnv_yo1kg5q"],
  "commentId": "com_1ywg3f2",
  "assigneeId": "tea_2thf",
  "tagIds": ["tag_13o8r1"]
}
```

A second Flow for the same conversation completes as `skipped` and writes
nothing, unless 25 or more comments were added after the triage comment, which
then falls outside the comments the Flow reads.

## Test

From `connectors/front`:

```bash
go test -race ./examples/conversation-triage/...
```

The unit tests cover the requester choice, the skip rule, the comment text
and its count across search pages, the configuration loading, every visible
configuration unit as Dex Web reads it from the Flow source, and the README
samples.

With the latest Dex development server running, the real-Dex tests drive the
Flow on a real Worker against a stateful fake Front that requires the bearer
token:

- a conversation is triaged with one comment, the tag, and the assignee, with
  the expected search, contact, message, `PATCH`, and tag requests, while the
  requester's other open, archived, and other-inbox conversations and another
  customer's conversation are untouched;
- a conversation an earlier Flow triaged is skipped before any lookup;
- a comment held past Dex's async local phase is sent once (the
  duplicate-dispatch test);
- a Worker lost while Front holds the comment leaves the next attempt to
  complete as `needsReview` without sending again. The dispatch checkpoint
  narrows but does not close this window: a Worker lost before Dex stores the
  checkpoint can still send the comment twice;
- a tag write held past the local phase is dispatched again and converges on
  one copy of the tag;
- a rate-limited search waits for `Retry-After`;
- a rejected comment fails the Flow without Front's message text;
- an unconfigured triage tag fails the Flow before any Front request.

Opening a connection from a loaded project needs project storage, so these
tests build the connection with a static credential.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/conversation-triage/... -count=1 -v
```
