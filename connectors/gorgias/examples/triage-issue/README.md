# Gorgias triage-issue example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Gorgias operation to triage a new customer issue:

1. `RecordCustomerIssue` validates the requester email, subject, message,
   issue tag, Gorgias priority, and optional acknowledgement and records the
   issue;
2. `FindRequester` calls `gorgias.NewFindCustomerByEmailStep`, and
   `ChooseRequesterRoute` opens a ticket for a customer Gorgias does not know;
3. otherwise `FindOpenIssueTicket` calls `gorgias.NewSearchTicketsStep` for
   the customer's `open` tickets that carry the issue tag, 100 per page, and
   `ChooseIssueTicket` picks the most recently updated one, reads the next
   page when this page holds none, up to five pages, or opens a ticket after
   the last page;
4. `ReadCandidateTicket` calls `gorgias.NewGetTicketStep`, and
   `ConfirmCandidateTicket` follows up only when the ticket, as just read,
   still belongs to the customer, carries the tag, and is open;
5. `PrioritizeIssueTicket` calls `gorgias.NewUpdateTicketStep` to keep the
   ticket open, set the requested priority, and tag it `dex-repeat-contact`;
6. otherwise `OpenIssueTicket` calls `gorgias.NewCreateTicketStep` with the
   message as the customer's first message and the requested priority;
7. `AddTriageNote` calls `gorgias.NewAddNoteStep` to add one internal triage
   note, and `RecordTriageNote` completes, or, for a followed-up ticket with an
   acknowledgement, `AcknowledgeCustomer` calls `gorgias.NewAddNoteStep` with
   `isPublicReply` to email it to the customer and `CompleteTriage` completes.

The `uncertain` branches of the create, the note, and the reply are wired: a
request that was sent and that Gorgias does not show completes the Flow with
`needsReview: true`, the `reviewReason`, and the connector's `reviewDetail`,
so a person checks Gorgias instead of the Flow sending a possible duplicate.
Every other optional branch is unwired and fails the Flow, such as a rejected
ticket, a missing ticket, an invalid response, or a local defect.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/gorgias` with the latest stable
dexcli release:

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
mkdir -p /tmp/gorgias-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/gorgias/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/gorgias \
  --version v0.21.0 --tag connectors/gorgias/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/gorgias-release/connector-release.json \
  --digest-output /tmp/gorgias-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/gorgias/build" \
  --connector-release-override gorgias=/tmp/gorgias-release
```

Follow the [Gorgias setup](../../README.md#gorgias-setup), then open
**Connections** in Dex Web and select `gorgias / gorgias-helpdesk`, which all
seven Gorgias Steps of `GorgiasTriageIssue` use. Enter the helpdesk domain,
the user's email, and the API key and save; the status becomes **Ready**. The
example has no Step configuration. Dex Web or Superverse Studio writes the
connection to the project configuration.

In a second terminal, start the Worker from `connectors/gorgias` with the
`DEX_PROJECT_*` environment that names that configuration, as
[project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
go run ./examples/triage-issue
```

The Worker reads the connection at startup and the credentials before every
call, so restart it after changing the domain. The default Worker address is
`127.0.0.1:8833`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. For local verification against a Gorgias-compatible fake only,
`GORGIAS_LOCAL_API_BASE_URL` replaces `https://{domain}.gorgias.com/api`; it
must be HTTPS or a loopback HTTP URL, and production Workers leave it unset.

In the Run workspace, choose **Start Flow**, select `GorgiasTriageIssue`,
choose the Worker at `127.0.0.1:8833`, enter a unique Flow ID, and submit.
`priority` is Gorgias's string: `low`, `normal`, `high`, or `critical`.

```json
{
  "requesterEmail": "jane@acme.example.com",
  "requesterName": "Jane Smith",
  "subject": "Double charge on order 88213",
  "message": "I was charged twice for order 88213.",
  "issueTag": "billing-double-charge",
  "priority": "high",
  "acknowledgement": "Thanks Jane, we are looking into the double charge."
}
```

Omit `acknowledgement` to send no email. The Flow sends it only when it
follows up on an existing ticket whose customer wrote by email; Gorgias emails
the reply asynchronously. The Flow result and the `gorgias-triage-outcome`
Attribute hold the action (`opened`, `followedUp`, or `creationUncertain`),
the ticket, the note and acknowledgement IDs, and the review state.

## Test

```bash
GOWORK=off go test -race ./examples/triage-issue/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake Gorgias that, like Gorgias,
has no idempotency key and lists tickets filtered only by customer:

- a new customer gets one ticket and one internal note, with no ticket list;
- a repeat contact reprioritizes and tags the customer's open ticket, adds one
  internal note, emails one acknowledgement from the address the customer
  wrote to, and leaves a closed duplicate with a refund note, another tag's
  ticket, another customer's ticket, and a look-alike address's ticket
  untouched;
- a first page of the customer's closed tickets leads to the second page;
- a create and a note held for nine seconds, past Dex's async local phase, are
  each sent exactly once, because both Steps are sync;
- an update held for nine seconds is dispatched again by async Dex, and both
  attempts write the same values without duplicating a tag;
- a lost create response, and a lost reply response, are found by their
  `external_id` on the retry and never resent;
- a create that fails with a 502 and that Gorgias still does not show after
  repeated lookups completes as `needsReview` and is never resent;
- a rate-limited create waits for `Retry-After`, looks for the ticket by its
  `external_id`, and creates one ticket;
- a Worker lost while Gorgias holds a create it already stored is replaced,
  and the new attempt finds the dispatch checkpoint and the ticket and sends
  nothing;
- a rejected ticket fails the Flow through the unwired `providerRejected`
  branch without Gorgias's message text;
- an invalid priority fails the Flow before any Gorgias request.

```bash
GOWORK=off go test -tags=integration ./examples/triage-issue/... -count=1 -v
```
