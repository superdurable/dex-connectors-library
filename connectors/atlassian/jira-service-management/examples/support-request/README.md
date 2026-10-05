# Jira Service Management support request example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Jira Service Management operation:

1. `RecordSupportRequest` validates the request and records it with the picked
   service desk and request type;
2. `FindRequester` calls `jiraservicemanagement.NewFindCustomerByEmailStep`, and
   `EvaluateRequester` uses the first active customer whose email address equals
   the requester's. `notFound`, or only inactive matches, completes as
   `requesterNotFound` without raising anything;
3. `FindOpenDuplicateTickets` calls `jiraservicemanagement.NewSearchTicketsStep`
   for that customer's open requests in the desk whose summary contains the
   requested summary, and `EvaluateDuplicateTickets` reuses one whose summary is
   exactly the same. A near-duplicate such as `Laptop will not boot (again)`, a
   resolved request, or another customer's request is not reused. Search is
   eventually consistent, so this is a business duplicate check, never the
   retry-safety mechanism;
4. `CreateSupportTicket` calls `jiraservicemanagement.NewCreateTicketStep` with
   sync durability and `RaiseOnBehalfOfAccountID` set to the customer.
   `created` records the key, `providerRejected` completes as `rejected` with
   the field IDs and error key, and `uncertain` parks the Flow for an operator;
5. `ReadBackSupportTicket` calls `jiraservicemanagement.NewGetTicketStep`, and
   `VerifySupportTicket` adopts the request with its SLA names;
6. `PlanNextSupportAction` chooses the next unfinished action:
   `LabelSupportTicket` (`updateTicket`: add labels, set priority),
   `AddInternalNote` (`addComment` with `IsPublic: false`), `SendPublicReply`
   (`addComment` with `IsPublic: true`), and `MoveSupportTicket`
   (`transitionTicket` by destination status). Each record Step returns to it.
   An uncertain note or reply is recorded and never re-sent, and
   `transitionUnavailable` completes with the transitions Jira offered.

Blank `labels` and `priorityName`, `internalNote`, `publicReply`, or
`destinationStatusName` skip that action. Unwired optional branches, such as a
rejected search, a missing request after a create, or a rejected label change,
fail the Flow.

## Reconciling an uncertain create

`uncertain` means the request may exist. The Flow records the connector Call ID
and the time the outcome was observed, sets the phase to `needsReconciliation`,
and waits. An operator searches the desk's queue for the customer's request
around that time, then runs one of two Actions, each requiring
`jsm-support-request.reconcile`:

- **Confirm created request** takes the key the operator found. The Flow reads
  it with `getTicket` and adopts it only when its project, customer, and summary
  match; otherwise it returns to the operator with a note.
- **Raise request again** is the only path that creates again, as a new Step
  execution with a new connector Call ID.

## Configure

Follow the [Jira Service Management connector setup](../../README.md), then
configure the `jsm-support` connection under **Connections** in Dex Web. Leave
`cloudId` blank when the authorization covers one site. Two pickers are
optional:

- **Support service desk** on the `FindRequester` Step saves the service desk
  ID, its project key, and its name; every Step uses that desk.
- **Support request type** on the `CreateSupportTicket` Step saves the desk ID,
  the request type ID, and its name. When both pickers are saved, the Worker
  refuses to start unless they name the same desk.

Leave a picker unsaved to use each Start Flow input's `serviceDeskId`,
`projectKey`, and `requestTypeId`. The Worker reads the saved values once at
startup, so restart it after saving.

## Run

Generate strict FDG 2.0 from `connectors/atlassian/jira-service-management`
with the latest stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/support-request/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/support-request
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration environment described in
[project configuration](../../../../../sdkgo/projectconfig/README.md); Dex Web or
Superverse Studio writes that configuration when you save the connection.

```bash
go run ./examples/support-request
```

The default Worker address is `127.0.0.1:8830`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `JiraServiceManagementSupportRequest` with a unique Flow ID:

```json
{
  "requesterEmail": "jane@acme.example.com",
  "summary": "Laptop will not boot",
  "description": "My laptop will not boot.\nError 0x7B.",
  "serviceDeskId": "10",
  "projectKey": "ITH",
  "requestTypeId": "25",
  "labels": ["hardware"],
  "priorityName": "High",
  "internalNote": "Check the warranty first.",
  "publicReply": "We are looking into it.",
  "destinationStatusName": "In progress"
}
```

The Flow result and the `jsm-support-request` Attribute hold the phase, the
customer, the read-back request with its SLA names, the labels, both comment
outcomes, and the final status.

## Test

```bash
GOWORK=off go test -race ./examples/support-request/...
```

With the latest Dex development server running, the integration test drives
the Flow on a real Worker against a stateful fake Jira Service Management: a new
request raised once for the customer beside a near-duplicate, another
customer's same-summary request, and a resolved duplicate, then read back with
its SLAs, labeled, noted internally, replied to publicly, and moved; the
customer's open request reused; an unknown requester completed without a
create; a rejected create; a rate-limited create retried after `Retry-After`; a
create timeout reconciled by confirming a missing, a mismatched, and then the
real request key without a second create; a 5xx create raised again only after
operator approval; a nine-second create and a nine-second public reply each
sent once under sync durability; a nine-second label update and a nine-second
transition whose backup attempts find the change already made and write
nothing; a destination the workflow does not offer; an internal note timeout
recorded without a resend; and a blank `cloudId` resolved to the only granted
site.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/support-request/... -count=1 -v
```
