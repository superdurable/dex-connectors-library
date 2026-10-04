# Freshdesk triage-issue example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Freshdesk operation to triage a new customer issue:

1. `RecordCustomerIssue` validates the requester email, subject, message,
   issue tag, and Freshdesk priority and records the issue;
2. `FindUnresolvedIssueTicket` calls `freshdesk.NewSearchTicketsStep` for the
   customer's tickets that carry the issue tag in Freshdesk's documented
   unresolved statuses, 2 (Open), 3 (Pending), 6 (Waiting on Customer), and
   7 (Waiting on Third Party), and `ChooseIssueTicket` picks the most recently
   updated one, reads the next page when this page holds none of the
   customer's tickets, or opens a ticket after the last page;
3. `ReadCandidateTicket` calls `freshdesk.NewGetTicketStep`, and
   `ConfirmCandidateTicket` follows up only when the ticket, as just read,
   still belongs to the customer, carries the tag, and is unresolved. Search
   lags changes by minutes and its pages may omit tags;
4. `PrioritizeIssueTicket` calls `freshdesk.NewUpdateTicketStep` to reopen the
   ticket (status 2), set the requested priority, and tag it
   `dex-repeat-contact`;
5. otherwise `OpenIssueTicket` calls `freshdesk.NewCreateTicketStep` with the
   message as the description and the requested priority;
6. `AddTriageNote` calls `freshdesk.NewAddNoteStep` to add one private triage
   note naming the priority as Freshdesk's label and integer, such as
   `High (3)`, and `CompleteTriage` completes with the outcome.

The `uncertain` branches of the create and the note are wired: a request that
was sent but whose outcome is unknown completes the Flow with
`needsReview: true`, the `reviewReason`, and the connector's `reviewDetail`, so
a person checks Freshdesk instead of the Flow sending a possible duplicate.
Every other optional branch is unwired and fails the Flow, such as a rejected
ticket, a missing ticket, an invalid response, or a local defect.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/freshworks/freshdesk` with the latest
stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/triage-issue/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/triage-issue
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
mkdir -p /tmp/freshdesk-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/freshworks/freshdesk/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk \
  --version v0.21.0 --tag connectors/freshworks/freshdesk/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/freshdesk-release/connector-release.json \
  --digest-output /tmp/freshdesk-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/freshworks/freshdesk/build" \
  --connector-release-override freshdesk=/tmp/freshdesk-release
```

Follow the [Freshdesk setup](../../README.md#freshdesk-setup), then open
**Connections** in Dex Web and select `freshdesk / freshdesk-helpdesk`, which
all five Freshdesk Steps of `FreshdeskTriageIssue` use. Enter the helpdesk
domain and the agent's API key and save; the status becomes **Ready**. The
example has no Step configuration. Dex Web or Superverse Studio writes the
connection to the project configuration.

In a second terminal, start the Worker from `connectors/freshworks/freshdesk`
with the `DEX_PROJECT_*` environment that names that configuration, as
[project configuration loading](../../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
go run ./examples/triage-issue
```

The Worker reads the connection at startup and the credentials before every
call, so restart it after changing the domain. The default Worker address is
`127.0.0.1:8832`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. For local verification against a Freshdesk-compatible fake only,
`FRESHDESK_LOCAL_API_BASE_URL` replaces `https://{domain}.freshdesk.com/api/v2`;
it must be HTTPS or a loopback HTTP URL, and production Workers leave it unset.

In the Run workspace, choose **Start Flow**, select `FreshdeskTriageIssue`,
choose the Worker at `127.0.0.1:8832`, enter a unique Flow ID, and submit.
`priority` is Freshdesk's integer: 1 (Low), 2 (Medium), 3 (High), or
4 (Urgent).

```json
{
  "requesterEmail": "jane@acme.example.com",
  "requesterName": "Jane Smith",
  "subject": "Double charge on order 88213",
  "message": "I was charged twice for order 88213.",
  "issueTag": "billing-double-charge",
  "priority": 3,
  "groupId": 156
}
```

Omit `groupId` to leave routing of a new ticket to Freshdesk. The Flow result
and the `freshdesk-triage-outcome` Attribute hold the action (`opened`,
`followedUp`, or `creationUncertain`), the ticket, the note ID, and the review
state.

## Test

```bash
GOWORK=off go test -race ./examples/triage-issue/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake Freshdesk that, like
Freshdesk, has no idempotency key and omits tags from search results:

- a new customer gets one ticket and one private note, with no search;
- a repeat contact reopens and reprioritizes the customer's pending ticket,
  adds one private note, and leaves a resolved duplicate with a refund note,
  another customer's ticket, and a look-alike address's ticket untouched;
- a search match whose tag the index still shows but the ticket no longer has
  is read, skipped, and never written;
- a first page of other customers' tickets leads to the second page;
- a create and a note held for nine seconds, past Dex's async local phase, are
  each sent exactly once, because both Steps are sync;
- an update held for nine seconds is dispatched again by async Dex, and both
  attempts write the same values without duplicating a tag;
- a lost update response is retried and finds the values applied;
- a lost create or note response completes as `needsReview` and is never resent;
- a rate-limited create waits for `Retry-After` and creates one ticket;
- a Worker lost while Freshdesk holds the create is replaced, and the new
  attempt finds the dispatch checkpoint and sends nothing;
- a rejected ticket fails the Flow through the unwired `providerRejected`
  branch without Freshdesk's message text;
- an invalid priority fails the Flow before any Freshdesk request.

```bash
GOWORK=off go test -tags=integration ./examples/triage-issue/... -count=1 -v
```
