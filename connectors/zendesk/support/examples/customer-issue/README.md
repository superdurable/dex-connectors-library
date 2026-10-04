# Zendesk Support customer-issue example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Zendesk Support operation:

1. `RecordCustomerIssue` validates the requester email, subject, message, and
   issue tag and records the issue;
2. `FindUnsolvedIssueTicket` calls `support.NewSearchTicketsStep` for the
   customer's `new`, `open`, `pending`, and `hold` tickets that carry the issue
   tag, and `ChooseIssueTicket` picks the most recently updated one;
3. `ReadCandidateTicket` calls `support.NewGetTicketStep`, and
   `ConfirmCandidateTicket` follows up only when the ticket, as just read,
   still belongs to the customer, carries the tag, and is unsolved. Search can
   lag a change by minutes and its requester match is not guaranteed exact;
4. `AddIssueFollowUp` calls `support.NewUpdateTicketStep` to add the message as
   one internal note, reopen the ticket, and tag it `dex-repeat-contact`, and
   `CompleteFollowUp` completes as `followedUp`;
5. otherwise `OpenIssueTicket` calls `support.NewCreateTicketStep` with the
   message as the first public comment, and `CompleteOpenedTicket` completes as
   `opened`, recording any search match it skipped.

Only the happy-path branches are wired. A rejected write, a missing ticket, an
invalid response, a stale `expectedUpdatedAt`, and a local defect fail the
Flow. A retried or re-dispatched write Step finds its own ticket or note, and
the outcome reports `wasAlreadyApplied`.

## Configure

Follow the [Zendesk Support setup](../../README.md#zendesk-setup), then open
**Connections** in Dex Web and configure the `zendesk-support-desk`
connection: the account subdomain, the agent email, and the API token. The
example has no Step configuration, and the Worker reads the connection at
startup and the credentials before every call.

## Run

Generate strict FDG 2.0 from `connectors/zendesk/support` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/customer-issue/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/customer-issue
```

Run `dexcli dev` with that build directory. Dex Web or Superverse Studio writes
the connection to the project configuration; run the Worker with the
`DEX_PROJECT_*` environment that names it, as
[project configuration loading](../../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
go run ./examples/customer-issue
```

The default Worker address is `127.0.0.1:8830`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. For local verification against a Zendesk-compatible fake only,
`ZENDESK_SUPPORT_LOCAL_API_BASE_URL` replaces
`https://{subdomain}.zendesk.com/api/v2`; it must be HTTPS or a loopback HTTP
URL, and production Workers leave it unset.

Start `ZendeskCustomerIssue` with a unique Flow ID:

```json
{
  "requesterEmail": "jane@acme.example.com",
  "requesterName": "Jane Smith",
  "subject": "Double charge on order 88213",
  "message": "I was charged twice for order 88213.",
  "issueTag": "billing-double-charge",
  "groupId": 98738
}
```

Omit `groupId` to leave routing to Zendesk. The Flow result and the
`zendesk-customer-issue-outcome` Attribute hold the action, the ticket, and
`wasAlreadyApplied`.

## Test

```bash
GOWORK=off go test -race ./examples/customer-issue/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake Zendesk that honors the
documented Idempotency-Key replay, safe updates, and audit metadata:

- a new issue opens one ticket with the expected query, key, and body;
- a repeat contact adds one internal note and leaves a solved duplicate and
  another customer's ticket untouched;
- a search match whose requester has a look-alike address is read, skipped,
  and never written;
- a lost create response is retried under the same key and replayed;
- a create held past Dex's async local phase is dispatched again, gets 409
  while the first request is in flight, and still creates one ticket;
- a lost update response is retried, finds its audit marker, and writes no
  second note;
- an update held past the local phase races a backup attempt, and the safe
  update rejects the stale write, so exactly one note is added;
- a rate-limited search waits for `Retry-After`;
- a rejected ticket fails the Flow through the unwired `providerRejected`
  branch without Zendesk's message text;
- an invalid email fails the Flow before any Zendesk request.

```bash
GOWORK=off go test -tags=integration ./examples/customer-issue/... -count=1 -v
```
