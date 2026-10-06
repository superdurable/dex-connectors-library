# Hiver claim-conversation example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Hiver operation to claim one conversation of a shared inbox for a Hiver
user:

1. `RecordClaimRequest` validates the shared inbox address, the assignee's
   email, the optional claim tag, the internal note, and the optional reply
   draft, and records the request;
2. `FindSharedInbox` calls `hiver.NewListInboxesStep`, and `ChooseSharedInbox`
   picks the inbox whose address matches, reads the next page, or fails the
   Flow after five pages without a match;
3. `FindUnassignedConversation` calls `hiver.NewListConversationsStep`, and
   `ChooseConversation` picks the first conversation that is `open` with no
   assignee, reads the next page, or completes with `nothingToClaim` after
   five pages of 50;
4. `ReadConversation` calls `hiver.NewGetConversationStep`, and
   `ConfirmConversation` claims the conversation only if, as just read, it is
   still open and unassigned; otherwise the Flow completes with
   `claimedElsewhere` and changes nothing;
5. `ClaimConversation` calls `hiver.NewUpdateConversationStep` to assign the
   conversation by email and apply the claim tag by name, and reads it back;
6. `AddClaimNote` calls `hiver.NewAddNoteStep` to add one internal note naming
   the assignee;
7. when a reply draft was entered, `DraftCustomerReply` calls
   `hiver.NewCreateSharedDraftStep` to leave a shared draft that replies to the
   conversation's last listed message, for the assignee to review and send;
   `CompleteClaim` completes with the outcome.

The `uncertain` branches of the note and the draft are wired: a request that
was sent but whose outcome is unknown completes the Flow with
`needsReview: true`, the `reviewReason`, and the connector's `reviewDetail`, so
a person checks Hiver instead of the Flow sending a possible duplicate. Every
other optional branch is unwired and fails the Flow, such as an assignee or tag
the inbox does not have, a missing conversation, an invalid response, or a
local defect.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/hiver` with the latest stable dexcli
release:

```bash
mkdir -p build
dexcli visualize ./examples/claim-conversation/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/claim-conversation
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
mkdir -p /tmp/hiver-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/hiver/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/hiver \
  --version v0.21.0 --tag connectors/hiver/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/hiver-release/connector-release.json \
  --digest-output /tmp/hiver-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/hiver/build" \
  --connector-release-override hiver=/tmp/hiver-release
```

Follow the [Hiver setup](../../README.md#hiver-setup), then open
**Connections** in Dex Web and select `hiver / hiver-account`, which all six
Hiver Steps of `HiverClaimConversation` use. Paste the admin's API key and
save; the status becomes **Ready**. Leave `maxResponseBytes` and
`requestIntervalMilliseconds` blank unless Hiver has raised your account's
rate limit. The example has no Step configuration. Dex Web or Superverse
Studio writes the connection to the project configuration.

In a second terminal, start the Worker from `connectors/hiver` with the
`DEX_PROJECT_*` environment that names that configuration, as
[project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
go run ./examples/claim-conversation
```

The Worker reads the connection settings at startup and the API key before
every call, so restart it after changing a setting. The default Worker address
is `127.0.0.1:8863`. Override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when needed. For local
verification against a Hiver-compatible fake only, `HIVER_LOCAL_API_BASE_URL`
replaces `https://api2.hiverhq.com/v1`; it must be HTTPS or a loopback HTTP
URL, and production Workers leave it unset.

In the Run workspace, choose **Start Flow**, select `HiverClaimConversation`,
choose the Worker at `127.0.0.1:8863`, enter a unique Flow ID, and submit.
`claimTagName` must name an existing tag of the inbox; leave it out to apply
none, and leave out `replyDraft` to create no draft.

```json
{
  "inboxEmail": "support@acme.example.com",
  "assigneeEmail": "phoebe@acme.example.com",
  "claimTagName": "Claimed",
  "note": "Taking this one; refund check first.",
  "replyDraft": "Hi Jane, we are looking into the double charge now."
}
```

The Flow result and the `hiver-claim-outcome` Attribute hold the action
(`claimed`, `nothingToClaim`, or `claimedElsewhere`), the inbox ID, the pages
read, the conversation as read back, the note and draft IDs, and the review
state.

## Test

```bash
GOWORK=off go test -race ./examples/claim-conversation/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake Hiver that, like Hiver, has
no idempotency key or note list and answers 429 to requests that arrive too
close together:

- a claim pages to the second inbox page and the second conversation page,
  assigns, tags, notes, and drafts once, and keeps Hiver's documented limit of
  one request per second without a 429, leaving later conversations
  unassigned;
- pages with no open unassigned conversation complete as `nothingToClaim`
  with no write;
- a conversation another agent claims between the list and the read is left
  alone as `claimedElsewhere`;
- an update held for nine seconds is dispatched again by async Dex, and both
  attempts send the same change without duplicating the tag;
- a note held for nine seconds, past Dex's async local phase, is sent exactly
  once, because the Step is sync;
- a lost note response completes as `needsReview` and is never resent;
- a rate-limited note waits for `Retry-After` and adds one note;
- a Worker lost while Hiver holds the draft is replaced, and the new attempt
  finds the dispatch checkpoint and sends nothing; a Worker lost before Dex
  stored the checkpoint could still send twice, as
  [Duplicate safety](../../README.md#duplicate-safety) explains;
- an assignee who is not an inbox user fails the Flow through the unwired
  `providerRejected` branch before any change;
- an unknown inbox address fails the Flow after the last inbox page;
- a blank note fails the Flow before any Hiver request.

```bash
GOWORK=off go test -tags=integration ./examples/claim-conversation/... -count=1 -v
```
