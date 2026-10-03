# Zoho Desk triage-issue example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Zoho Desk operation to triage a new customer issue:

1. `RecordCustomerIssue` validates the contact email, subject, message,
   department, and Zoho Desk priority and records the issue;
2. `FindUnresolvedIssueTicket` calls `desk.NewSearchTicketsStep` for the
   contact's tickets in the department whose status type is Open or On Hold,
   so custom statuses match, and `ChooseIssueTicket` picks the most recently
   modified one, reads the next page when this page holds none of the
   contact's tickets, or opens a ticket after the last page;
3. `ReadCandidateTicket` calls `desk.NewGetTicketStep`, and
   `ConfirmCandidateTicket` follows up only when the ticket, as just read,
   still belongs to the contact, is in the department, and is not closed.
   Zoho Desk's search index lags changes;
4. `PrioritizeIssueTicket` calls `desk.NewUpdateTicketStep` to set the status
   `Open` and the requested priority;
5. otherwise `OpenIssueTicket` calls `desk.NewCreateTicketStep` with the
   message as the description, the department, and the priority;
6. `AddTriageComment` calls `desk.NewAddCommentStep` to add one private
   comment, and `CompleteTriage` completes with the outcome.

The `uncertain` branches of the create and the comment are wired: a request
that was sent but whose outcome is unknown completes the Flow with
`needsReview: true`, the `reviewReason`, and the connector's `reviewDetail`, so
a person checks Zoho Desk instead of the Flow sending a possible duplicate.
Every other optional branch is unwired and fails the Flow, such as a rejected
ticket, a missing ticket, an invalid response, or a local defect.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/zoho/desk` with the latest stable
dexcli release:

```bash
mkdir -p /tmp/zoho-desk-render
dexcli visualize ./examples/triage-issue/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/zoho-desk-render/triage-issue
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` for each connector Step, because a local module
is not a published release.

## Configure and run

This example is part of the connector module, so Dex needs release metadata
and the Studio bundle built from this source, passed as an override; without
them the connection shows **Unsupported**. From the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
npm ci --prefix sdk/react && npm run build --prefix sdk/react
npm ci --prefix connectors/zoho/desk/ui && npm run build --prefix connectors/zoho/desk/ui
mkdir -p /tmp/zoho-desk-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/zoho/desk/connector.yaml \
  --ui-root connectors/zoho/desk/ui/dist \
  --output /tmp/zoho-desk-release/connector-ui.tgz \
  --digest-output /tmp/zoho-desk-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/zoho/desk/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/zoho/desk \
  --version v0.1.0 --tag connectors/zoho/desk/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/zoho-desk-release/connector-ui.tgz \
  --ui-digest /tmp/zoho-desk-release/connector-ui.tgz.sha256 \
  --output /tmp/zoho-desk-release/connector-release.json \
  --digest-output /tmp/zoho-desk-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp/zoho-desk-render \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override zoho-desk=/tmp/zoho-desk-release
```

Follow the [Zoho setup](../../README.md#zoho-setup), then open
**Connections** in Dex Web and select `zoho-desk / zoho-desk-helpdesk`, which
all five Zoho Desk Steps of `ZohoDeskTriageIssue` use. Choose the
**Data center** of your Zoho Desk account, enter the client ID and secret, and
choose **Authorize**. When the status is **Ready**, choose the organization in
`orgId` with the organization picker and save. The example has no Step
configuration. The tokens are stored only in the plaintext development file
shown on the page; never commit or share it.

In a second terminal, start the Worker from `connectors/zoho/desk` with that
file:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/triage-issue
```

The Worker reads the connection at startup and the credentials before every
call, so restart it after changing `orgId`; it refuses to start while `orgId`
is blank. The default Worker address is `127.0.0.1:8856`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. For local verification against a Zoho Desk-compatible fake only,
`ZOHO_DESK_LOCAL_API_BASE_URL` replaces the data center's
`https://desk.zoho.<domain>/api/v1`; it must be HTTPS or a loopback HTTP URL,
and production Workers leave it unset.

In the Run workspace, choose **Start Flow**, select `ZohoDeskTriageIssue`,
choose the Worker at `127.0.0.1:8856`, enter a unique Flow ID, and submit.
`departmentId` is the numeric ID of a department, which
`GET /api/v1/departments` lists; `priority` is a Zoho Desk priority name, and
Start Flow offers the defaults High, Medium, and Low.

```json
{
  "contactEmail": "jane@acme.example.com",
  "contactFirstName": "Jane",
  "contactLastName": "Smith",
  "subject": "Double charge on order 88213",
  "message": "I was charged twice for order 88213.",
  "departmentId": "1892000000006907",
  "priority": "High"
}
```

The Flow result and the `zoho-desk-triage-outcome` Attribute hold the action
(`opened`, `followedUp`, or `creationUncertain`), the ticket, the comment ID,
and the review state.

## Test

```bash
GOWORK=off go test -race ./examples/triage-issue/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake Zoho Desk that, like Zoho
Desk, has no idempotency key, sends IDs and counts as strings, answers an
empty search with 204, and matches look-alike addresses in its email filter:

- a new contact gets one ticket and one private comment;
- a repeat contact's on-hold ticket is reopened and reprioritized with one
  private comment, leaving a closed duplicate with a refund comment, another
  contact's ticket, a look-alike address's newer ticket, and the contact's
  ticket in another department untouched;
- a search match the index still shows as open but that is closed is read,
  skipped, and never written;
- a first page of look-alike addresses leads to the page at `from=25`;
- a create and a comment held for nine seconds, past Dex's async local phase,
  are each sent exactly once, because both Steps are sync;
- an update held for nine seconds is dispatched again by async Dex, and both
  attempts write the same values;
- a lost update response is retried and finds the values applied;
- a lost create or comment response completes as `needsReview` and is never
  resent;
- a rate-limited create waits for `Retry-After` and creates one ticket;
- a Worker lost while Zoho Desk holds the create is replaced, and the new
  attempt finds the dispatch checkpoint and sends nothing;
- a token Zoho Desk rejects at the create is refreshed once through the local
  refreshing credential provider at the EU Zoho Accounts server, the create is
  sent once more, and the rewritten connection file keeps Dex Web's
  `authMethodId` and the unrotated refresh token;
- a rejected ticket fails the Flow through the unwired `providerRejected`
  branch without Zoho Desk's message text;
- an invalid department fails the Flow before any Zoho Desk request.

```bash
GOWORK=off go test -tags=integration ./examples/triage-issue/... -count=1 -v
```
