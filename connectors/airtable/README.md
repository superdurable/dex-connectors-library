# Airtable Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Airtable; no live Airtable base was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The Airtable Connector reads and writes records in Airtable bases through the
[Airtable Web API](https://airtable.com/developers/web/api/introduction). It
exposes four operation-specific Dex Step factories:

| Factory | Kind | Airtable request | Happy branch |
| --- | --- | --- | --- |
| `airtable.NewListRecordsStep` | Query | `POST /v0/{baseId}/{tableIdOrName}/listRecords` | `listed` |
| `airtable.NewGetRecordStep` | Query | `GET /v0/{baseId}/{tableIdOrName}/{recordId}` | `found` |
| `airtable.NewUpsertRecordsStep` | Mutation | `PATCH /v0/{baseId}/{tableIdOrName}` with `performUpsert` | `upserted` |
| `airtable.NewUpdateRecordsStep` | Mutation | `PATCH /v0/{baseId}/{tableIdOrName}` with record IDs | `updated` |

Airtable is a typed table store: fields have types, and linked record fields
hold the IDs of records in another table of the same base. The connector keeps
that shape instead of flattening cells to strings the way a spreadsheet does.
`CellValue` carries Airtable's JSON cell format. Write values with
`TextCellValue`, `NumberCellValue`, `CheckboxCellValue`,
`LinkedRecordsCellValue`, `MultipleSelectsCellValue`, and `NullCellValue`, and
read them with `Text`, `Number`, `Checkbox`, and `StringList`. Airtable omits
empty fields from every record it returns, so a missing key means an empty
field. A write rejects the zero `CellValue`, so a forgotten value never clears
a field.

## Authentication

A connection uses one Airtable personal access token (`airtable-personal-access-token`)
sent as `Authorization: Bearer`. Create it at
<https://airtable.com/create/tokens> with the scopes `data.records:read`,
`data.records:write`, and `schema.bases:read`, and add every base the Flows
use under **Access**. The token acts as the user who created it, so that user
needs editor access to those bases. Regenerating or deleting the token rejects
the old value; paste the replacement into the connection, and the running
Worker uses it on its next call. Hosted applications decode the broker's
operation-scoped token with `DecodeResolvedCredentialsJSON`.

Airtable OAuth is not shipped. Airtable's token endpoint,
`https://airtable.com/oauth2/v1/token`, requires the client credentials as
HTTP Basic whenever the integration has a client secret and forbids them
otherwise, as both the
[OAuth reference](https://airtable.com/developers/web/api/oauth-reference) and
Airtable's official `oauth-example` show. Dex Web `cli-v1.2.0` sends
`client_id` and `client_secret` in the form body of the authorization code
exchange, with an empty secret when its field is optional, and has no
manifest setting for HTTP Basic. An integration with a secret would therefore
fail the exchange, and one without a secret would depend on Airtable ignoring
an empty `client_secret`, which its documentation does not say. Airtable
access tokens last 60 minutes, refresh tokens 60 days, and
every refresh rotates both; a later release can add an OAuth method that
refreshes with `sdkgo/oauthtoken` and `ClientSecretBasic` once Dex Web can
complete the exchange.

## Operations

### listRecords

`ListRecordsInput` selects one page of at most `PageSize` records, 1 to 100
(zero means 100), and returns `RecordPage.Offset` for the next page; a blank
offset means the last page. Every parameter travels in the JSON body of
Airtable's `POST listRecords` form, so a long formula never meets Airtable's
16,000-character URL limit.

`Filters` hold typed equality filters, built with `TextFieldEquals`,
`NumberFieldEquals`, and `CheckboxFieldEquals`. The connector writes each one
as an Airtable formula and joins them, and an optional raw `Formula`, with
`AND` into `filterByFormula`; `RecordPage.FilterFormula` reports what it sent.
Text is quoted with Airtable's documented `\"` escape. Airtable documents no
escape for a backslash, a line break, or a brace inside a `{field}` reference,
so a typed filter rejects those characters as `defect`; use the field ID, such
as `fldXXXXXXXXXXXXXX`, or a raw `Formula` instead. A raw formula is sent as
written, so build it from application code, never from end-user text.
`Fields` limits the returned fields, `Sort` orders records and overrides
`ViewIDOrName`'s order, and without either Airtable returns records in no
particular order.

When Airtable no longer recognizes an offset (`422
LIST_RECORDS_ITERATOR_NOT_AVAILABLE`), the operation selects the optional
`offsetExpired` branch, and the listing must restart from the first page.

### getRecord

`GetRecordInput` reads one record by record ID. A missing record selects
`notFound`. Airtable falls back to a base-wide lookup when the record is not
in the named table, so a record ID from another table of the same base is
still returned; the response does not name the table.

### upsertRecords

`UpsertRecordsInput` writes up to 10 records matched by one to three
`FieldsToMergeOn`. Airtable creates a record when no existing record matches
the record's values for those fields, updates the one record that matches,
and rejects the whole request when several match. Airtable lists number,
text, long text, single select, multiple select, and date fields as merge
fields, never computed ones such as formulas, lookups, or rollups. Every record holds
a non-null value for every merge field, and no two records in one request
share merge values.

Airtable has no idempotency key. The merge fields make the write repeat-safe:
a retry finds the record the first attempt created and updates it with the
same values, so the operation retries every ambiguous outcome and declares no
`uncertain` branch. `UpsertedRecords.CreatedRecordIDs` describes the answered
request; after a lost response, the record the first attempt created appears
in `UpdatedRecordIDs`.

Airtable does not enforce a unique merge key, and its documentation does not
say that concurrent upserts are serialized. Under the default async
durability, Dex dispatches a Step again when its local attempt passes about
seven seconds while the first request may still be in flight, so two
concurrent upserts could each find no match and both create a record. The
operation therefore defaults to sync durability, which never runs two attempts
of one Step at once. A real-Dex test with a fake Airtable that evaluates the
merge on arrival and commits it nine seconds later sends one upsert under
sync; the same test with async durability sends two. A residual race remains
when a Worker is lost mid-request and its retry reaches Airtable before the
abandoned request finishes; the next upsert of that key then selects
`providerRejected` for several matches instead of writing the wrong record.

No plain `createRecords` operation ships: without an idempotency key or a
merge key, a retried create cannot tell whether the first one landed. Give an
append-only log a stable key field, such as a case or event ID, and upsert it.

### updateRecords

`UpdateRecordsInput` sets the named fields of up to 10 records by record ID
and leaves every other field unchanged. Each value replaces the whole field,
including every link of a linked record field, so repeating an update is safe;
the operation keeps async durability, and a real-Dex test proves a slow update
dispatched twice converges. The request never sets `performUpsert`, so an
unknown record ID is never created; a `404` selects `notFound`.

### Branches and failures

| Airtable outcome | Branch or Retry | Failure kind |
| --- | --- | --- |
| `2xx` with a valid body | the happy branch | none |
| `401` | `providerRejected` | `AUTHENTICATION` |
| `403`, such as `INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND` | `providerRejected` | `AUTHORIZATION` |
| `404` | `notFound` on `getRecord` and `updateRecords`, otherwise `providerRejected` | `NOT_FOUND` |
| `422 LIST_RECORDS_ITERATOR_NOT_AVAILABLE` | `offsetExpired` on `listRecords` | `NOT_FOUND` |
| `400`, `413`, other `422` | `providerRejected` | `VALIDATION` |
| `429` | Retry after at least 30 seconds | `RATE_LIMIT` |
| `408`, `5xx` except `501`, a dropped connection | Retry, honoring `Retry-After` | `AVAILABILITY` or `TRANSPORT` |
| `3xx`, `501`, other `4xx` | `providerRejected` | `PROTOCOL` or `PROVIDER_REJECTION` |
| Malformed, inconsistent, or oversized `2xx` | `invalidResponse` | `PROTOCOL` or `RESPONSE_TOO_LARGE` |
| Invalid input or connection | `defect`, with no request | `VALIDATION` or `AUTHENTICATION` |

A Failure names only the HTTP status and Airtable's uppercase error type, such
as `INVALID_VALUE_FOR_COLUMN`, never Airtable's message text or the token. A
write that saved records but not every attachment (`partialSuccess`) still
selects its happy branch and lists the documented reasons in
`PartialSuccessReasons`.

### Rate limits

Airtable allows 5 requests per second per base and asks a rate-limited client
to wait 30 seconds. A `Client` spaces its requests to each base 200
milliseconds apart; a Step whose slot is more than two seconds away returns
Retry instead of waiting. After a `429`, the client holds that base for 30
seconds, so other Steps in the same process do not extend the penalty. Other
processes that share a base are not coordinated. Every operation's retry
policy allows three minutes, which fits several 30-second waits.

## Studio configuration

The `ui/` bundle provides two release-owned units:

- `basePicker` lists the bases the token can reach through the read-only
  `listBases` command, `GET https://api.airtable.com/v0/meta/bases`, and stores
  `baseId` and `baseName`. It follows Airtable's `offset` cursor for up to 20
  pages of 1,000 bases and offers manual base ID entry beyond that.
- `tablePicker` reads the saved `baseId` and lists that base's tables through
  `listTables`, `GET https://api.airtable.com/v0/meta/bases/{baseId}/tables`,
  and stores the stable `tableId`, which keeps working after a rename, and
  `tableName`.

Both commands send the token as a bearer credential that Dex Web injects; the
iframe never receives it. Dex Web bounds a command response at 4 MiB, so a
base whose schema is larger cannot list its tables, and the unit falls back to
manual table ID entry. The commands always call Airtable's public API host,
whatever the connection's `endpoint`.

| Connection field | Default | Purpose |
| --- | --- | --- |
| `endpoint` | `https://api.airtable.com` | Airtable Web API base URL for operations. |
| `maxResponseBytes` | `4194304` | Largest complete response; larger ones select `invalidResponse`. |
| `personal_access_token` | none | Secret personal access token. |

## Triggers

No Trigger ships. An Airtable webhook does not deliver changes: it sends a
ping that names the base and webhook, signed with an HMAC of the body in
`X-Airtable-Content-MAC`, and the receiver then lists the changes with
`GET /v0/bases/{baseId}/webhooks/{webhookId}/payloads` from a stored cursor.
Payloads are kept for seven days, pings are at-least-once and unordered, and
a webhook created with a token expires after seven days unless it is
refreshed. A Trigger therefore needs more than `webhooktrigger.Endpoint`,
which verifies and decodes the pushed request itself:

- a binding setup Mutation that creates the webhook and stores its
  `macSecretBase64` as a credential, because the MAC secret is not a value the
  user can paste;
- a durable per-webhook payload cursor, advanced only after every payload in
  a page is recorded, and a pull loop that follows `mightHaveMore`;
- a refresh at least every seven days, with the `webhook:manage` scope;
- a decoder that turns each changed record into a stable event ID from the
  payload's `baseTransactionNumber` and record ID.

The `sdkgo` webhook and Trigger packages have no ping-then-pull source today.

## Example

[`examples/refund-decision`](examples/refund-decision) runs one Flow from Dex
Web **Start Flow** that uses every operation: it reads the refund policy row
whose key matches the request, decides the refund, upserts the case's decision
log row with a link to the policy, stamps the policy with the case, and reads
the log row back.

The Worker builds the connection from the Dex Web connection file:

```go
connection, err := airtable.NewLocalConnection(store, refunddecision.ConnectionName)
if err != nil {
	return err
}
```

The policy lookup lists two rows, so a duplicate policy key is visible
instead of silently choosing one:

```go
// MapToFindRefundPolicyInput lists up to two policies with the request's key, so a duplicate key is visible.
func (flow *Flow) MapToFindRefundPolicyInput(decision RefundDecision) airtable.ListRecordsInput {
	return airtable.ListRecordsInput{
		BaseID: flow.settings.PolicyTable.BaseID, TableIDOrName: flow.settings.PolicyTable.TableID,
		Filters:  []airtable.FieldEqualityFilter{airtable.TextFieldEquals(PolicyKeyField, decision.Request.PolicyKey)},
		Fields:   []string{PolicyKeyField, ApprovalLimitField},
		PageSize: policyPageSizeThatRevealsAmbiguity,
	}
}
```

A typed cell is read back with its reader:

```go
limit, hasLimit := policies[0].Fields[ApprovalLimitField].Number()
if !hasLimit {
	decision.Decision, decision.Reason = DecisionNeedsReview, "the policy has no Approval Limit USD"
	return decision
}
```

The decision log row is merged on its case ID and links the policy record:

```go
// MapToUpsertDecisionLogInput writes the case's log row, merged on Case ID, linking the matched policy or none.
func (flow *Flow) MapToUpsertDecisionLogInput(decision RefundDecision) airtable.UpsertRecordsInput {
	policyLinks := airtable.LinkedRecordsCellValue()
	if decision.PolicyRecordID != "" {
		policyLinks = airtable.LinkedRecordsCellValue(decision.PolicyRecordID)
	}
	return airtable.UpsertRecordsInput{
		BaseID: flow.settings.LogTable.BaseID, TableIDOrName: flow.settings.LogTable.TableID,
		FieldsToMergeOn: []string{CaseIDField},
		Records: []airtable.RecordUpsert{{Fields: map[string]airtable.CellValue{
			CaseIDField:     airtable.TextCellValue(decision.Request.CaseID),
			CustomerField:   airtable.TextCellValue(decision.Request.Customer),
			AmountField:     airtable.NumberCellValue(decision.Request.AmountUSD),
			DecisionField:   airtable.TextCellValue(string(decision.Decision)),
			PolicyLinkField: policyLinks,
		}}},
	}
}
```

The example README lists the table fields, the Dex Web setup, and the Start
Flow input.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

The real-Dex example tests need a Dex development server:

```bash
dexcli dev
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/refund-decision/... -count=1 -v
```

They cover an approved case end to end with a duplicate start, a rerun of one
case that updates its log row, a duplicate policy key logged for review, a
nine-second provider that receives one sync upsert and two converging async
updates, a `429` retried after Airtable's 30-second cooldown, and a rejected
formula that fails the Flow without provider text.

No live Airtable base was used. The operations, picker commands, error types,
and limits follow Airtable's published Web API reference; the 10-record write
limit is enforced as Airtable has documented it, although the current
reference pages no longer state it.
