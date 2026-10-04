# Airtable refund-decision example

This example runs one operation-only Flow, `AirtableRefundDecision`, from Dex
Web **Start Flow** and uses every Airtable operation:

1. `RecordAirtableRefundRequest` validates and records the refund case.
2. `FindAirtableRefundPolicy` calls `airtable.NewListRecordsStep` for up to two
   policy rows whose `Policy Key` equals the request's key.
3. `DecideAirtableRefund` approves the amount within the one matching policy's
   `Approval Limit USD`, escalates it above the limit, and marks the case
   `needsReview` when no policy, several policies, or no limit matches.
4. `UpsertAirtableRefundDecisionLog` calls `airtable.NewUpsertRecordsStep` to
   create or update the case's decision log row, merged on `Case ID`, with a
   link to the matched policy.
5. `RecordAirtableRefundDecisionLog` records the row, then stamps the matched
   policy or goes straight to the read-back.
6. `StampAirtableRefundPolicy` calls `airtable.NewUpdateRecordsStep` to set the
   policy's `Last Decided Case`, and `RecordAirtableRefundPolicyStamp` checks it.
7. `ReadBackAirtableRefundDecisionLog` calls `airtable.NewGetRecordStep`, and
   `CompleteAirtableRefundDecision` confirms the stored case, decision, and
   policy link before completing.

Only happy-path branches are wired. A rejected request, an invalid response,
an expired offset, a missing record, and a local defect fail the Flow.

## Airtable base

Create both tables in one base, because Airtable links records only within a
base:

| Table | Field | Type |
| --- | --- | --- |
| Policies | `Policy Key` | Single line text, the primary field |
| Policies | `Approval Limit USD` | Number or currency |
| Policies | `Last Decided Case` | Single line text |
| Decision Log | `Case ID` | Single line text, the primary field |
| Decision Log | `Customer` | Single line text |
| Decision Log | `Amount USD` | Number or currency |
| Decision Log | `Decision` | Single line text |
| Decision Log | `Policy` | Link to another record, linked to Policies |

Add a policy row, such as `refund-standard` with an approval limit of `250`.

## Run it

Generate strict FDG 2.0 from `connectors/airtable`:

```bash
mkdir -p build
dexcli visualize ./examples/refund-decision/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/refund-decision
```

Run `dexcli dev` with that build directory and open Dex Web **Connections**.
Configure `airtable / airtable-refund-policies` with a personal access token
from <https://airtable.com/create/tokens> that has the `data.records:read`,
`data.records:write`, and `schema.bases:read` scopes and access to the base.
Then configure the two Airtable Steps:

- `FindAirtableRefundPolicy`: choose the base with **Policy base** and the
  Policies table with **Policy table**.
- `UpsertAirtableRefundDecisionLog`: choose the same base with **Decision log
  base** and the Decision Log table with **Decision log table**.

Dex Web or Superverse Studio writes the connection and both table picks to the
project configuration. Run the Worker with the `DEX_PROJECT_*` environment that
names it, as [project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
describes. The Worker loads both table picks once at startup and refuses to
start until both are saved and name the same base:

```bash
go run ./examples/refund-decision
```

Start `AirtableRefundDecision` with a unique Flow ID:

```json
{
  "caseId": "case-4471",
  "customer": "Jane Doe",
  "amountUsd": 120,
  "policyKey": "refund-standard"
}
```

The Flow completes with `decision` set to `approved`, the policy and log
record IDs, and `logRecordCreated: true`. Starting another Flow for the same
`caseId` updates the same log row instead of adding one.

## Tests

```bash
GOWORK=off go test -race ./examples/refund-decision/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/refund-decision/... -count=1 -v
```

The integration tests run the Flow on a real Dex Server against a stateful
fake Airtable, including a provider that answers writes after nine seconds and
a `429` that Dex retries after Airtable's 30-second cooldown.
