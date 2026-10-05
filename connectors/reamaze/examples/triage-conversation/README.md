# Re:amaze triage-conversation example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Re:amaze operation to triage a new customer issue:

1. `RecordCustomerIssue` validates the requester email, subject, message,
   issue tag, and channel slug and records the issue;
2. `FindCustomerContact` calls `reamaze.NewFindContactByEmailStep`, and
   `RouteCustomerContact` opens a conversation for a customer Re:amaze does not
   know, or searches a known customer's conversations;
3. `FindUnresolvedIssueConversation` calls `reamaze.NewSearchConversationsStep`
   for the customer's unarchived conversations that carry the issue tag, most
   recently changed first, and `ChooseIssueConversation` picks the first one
   the customer started in an unresolved status, Open (0), Responded (1),
   On Hold (5), or AI Agent Assigned (7). It reads the next page when this one
   has none, up to five pages, then opens a conversation;
4. `ReadCandidateConversation` calls `reamaze.NewGetConversationStep`, and
   `ConfirmCandidateConversation` follows up only when the conversation, as
   just read, was started by the customer, carries the tag, and is
   unresolved. Re:amaze's requester filter also matches conversations the
   customer was copied on;
5. `ReopenIssueConversation` calls `reamaze.NewUpdateConversationStep` to set
   the status to Open (0) and tag it `dex-repeat-contact`;
6. otherwise `OpenIssueConversation` calls `reamaze.NewCreateConversationStep`
   in the requested channel with the message as the first message and the
   issue tag;
7. `AddTriageNote` calls `reamaze.NewReplyToConversationStep` to add one
   internal note, with `shouldSuppressAutoResolve`, and `CompleteTriage`
   completes with the outcome. The note on a followed-up conversation names
   the status it set as Re:amaze's label and integer, such as `Open (0)`; the
   note on a newly opened conversation records why it was opened.

The `uncertain` branches of the create and the note are wired: a request that
was sent, whose outcome is unknown, and that the read-back did not find
completes the Flow with `needsReview: true`, the `reviewReason`, and the
connector's `reviewDetail`, so a person checks Re:amaze instead of the Flow
sending a possible duplicate. Every other optional branch is unwired and fails
the Flow, such as a rejected conversation, a missing conversation, an invalid
response, or a local defect.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/reamaze` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/triage-conversation/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/triage-conversation
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` for each connector Step, because a local module
is not a published release.

## Configure and run

This example is part of the connector module, so Dex needs release metadata
built from this source, passed as an override; without it the connection
shows **Unsupported**. The connector has no Studio bundle. From the repository
root:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/reamaze-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/reamaze/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/reamaze \
  --version v0.21.0 --tag connectors/reamaze/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/reamaze-release/connector-release.json \
  --digest-output /tmp/reamaze-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/reamaze/build" \
  --connector-release-override reamaze=/tmp/reamaze-release
```

Follow the [Re:amaze setup](../../README.md#reamaze-setup), then open
**Connections** in Dex Web and select `reamaze / reamaze-brand`, which all six
Re:amaze Steps of `ReamazeTriageConversation` use. Enter the brand subdomain,
the staff user's login email, and that user's API token and save; the status
becomes **Ready**. The example has no Step configuration. Dex Web or
Superverse Studio writes the connection to the project configuration.

In a second terminal, start the Worker from `connectors/reamaze` with the
`DEX_PROJECT_*` environment that names that configuration, as
[project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
go run ./examples/triage-conversation
```

The Worker reads the connection at startup and the credentials before every
call, so restart it after changing the brand. The default Worker address is
`127.0.0.1:8834`. Override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when needed. For local
verification against a Re:amaze-compatible fake only,
`REAMAZE_LOCAL_API_BASE_URL` replaces `https://{brand}.reamaze.io/api/v1`; it
must be HTTPS or a loopback HTTP URL, and production Workers leave it unset.

In the Run workspace, choose **Start Flow**, select
`ReamazeTriageConversation`, choose the Worker at `127.0.0.1:8834`, enter a
unique Flow ID, and submit. `channel` is the slug of an email or chat channel
from Re:amaze's Settings > Channels.

```json
{
  "requesterEmail": "jane@acme.example.com",
  "requesterName": "Jane Smith",
  "subject": "Double charge on order 88213",
  "message": "I was charged twice for order 88213.",
  "issueTag": "billing-double-charge",
  "channel": "support"
}
```

The Flow result and the `reamaze-triage-outcome` Attribute hold the action
(`opened`, `followedUp`, or `creationUncertain`), the conversation, whether a
write was found already applied, the note's `origin_id`, and the review state.

## Test

```bash
GOWORK=off go test -race ./examples/triage-conversation/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake Re:amaze that, like
Re:amaze, has no idempotency key and whose requester filter also matches
conversations the customer follows:

- a new customer gets one conversation and one internal note, with no search;
- a repeat contact reopens the customer's Responded conversation, adds one
  internal note, and leaves a Done duplicate with a refund note, another
  customer's conversation the customer was copied on, and a look-alike
  address's conversation untouched;
- a first page of Done conversations leads to the second page;
- a create and a note held for nine seconds, past Dex's async local phase, are
  each sent exactly once, because both Steps are sync;
- an update held for nine seconds is dispatched again by async Dex, and both
  attempts write the same values without duplicating a tag;
- a lost create response is reconciled by the conversation's dispatch key,
  which the fake, like Re:amaze's documentation, returns only on a single
  conversation read, and the Flow continues without resending;
- a 502 that applied nothing completes as `needsReview` after one read-back
  and is never resent;
- a lost note response is reconciled by its `origin_id`;
- a rate-limited create waits for `Retry-After` and creates one conversation;
- a Worker lost while Re:amaze holds the create is replaced, and the new
  attempt finds the dispatch checkpoint, reads back, and sends nothing;
- a rejected conversation fails the Flow through the unwired
  `providerRejected` branch without Re:amaze's body text;
- an invalid channel fails the Flow before any Re:amaze request.

```bash
GOWORK=off go test -tags=integration ./examples/triage-conversation/... -count=1 -v
```
