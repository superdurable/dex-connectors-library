# Notion Connector

The Notion connector runs Notion's public REST API as Dex Steps. It can search
the pages and data sources shared with a connection by title, query a
database's rows with typed filters, read a page's properties and plain-text
content, create database rows, and update page properties. Every request
sends `Notion-Version: 2026-03-11`, Notion's current version, and the request
and response shapes follow it.

| Operation | Kind | Required branch | Durability |
| --- | --- | --- | --- |
| `search` | Query | `searched` | async |
| `queryDatabase` | Query | `queried` | async |
| `getPage` | Query | `found` | async |
| `createPage` | Mutation | `created` | sync |
| `updatePageProperties` | Mutation | `updated` | async |

Since `2025-09-03`, a Notion database is a container for one or more data
sources, and a database row is a page whose parent is a data source.
`queryDatabase` and `createPage` accept either a data source ID or a database
ID. A database ID resolves, with one extra read, to the database's only data
source. A database with several data sources selects `defect` and names their
IDs, so the caller can pick one.

The connector ships no Triggers and no Studio pickers; see
[Not in this release](#not-in-this-release).

## Notion setup

The connector authenticates with the API token of a Notion internal
connection. Notion's docs now call integrations *connections*.

1. Sign in as a Workspace Owner and open the Developer portal at
   <https://app.notion.com/developers/connections>. In **Build**, select
   **Internal connections**, choose **Create a new connection**, name it, and
   pick the workspace.
2. On the connection's **Configuration** tab, enable **Read content**,
   **Update content**, and **Insert content**. The connector reads no users
   or comments.
3. Copy the **API token** from the same tab. New tokens start with `ntn_`;
   older ones start with `secret_`. **Refresh** rotates the token, and Notion
   then rejects the old one.
4. Share every database and page the Flows use with the connection. Use the
   connection's **Content access** tab, or open the page in Notion and choose
   **•••** > **Connections** > **+ Add connection**. Sharing a page shares its
   children. Notion reports an unshared object as `object_not_found`, so the
   connector's `notFound` branches name sharing as a likely cause.

A Notion personal access token also works in the same field. It acts as its
creator and has that user's page permissions instead of the connection's.

### Finding IDs

Notion has no GET endpoint that lists databases or data sources, so the
connector cannot offer a picker. It accepts the values users can copy, and
`notion.ParseID` normalizes them to the dashed form:

- a database ID from the database's address bar, with or without dashes, or
  the URL itself, such as `https://www.notion.so/acme/Form-submissions-1f3c…?v=…`;
  the view parameter `v` is ignored;
- a page URL such as `https://app.notion.com/p/1f3c…`;
- a data source ID from the database's settings menu > **Manage data
  sources** > **Copy data source ID**;
- a title, found through `search`, as the example does.

The title property's ID is always `title`, whatever the column is named, so
`notion.TitlePropertyID` writes or filters the title without knowing its name.

## Local configuration

Dex Web writes the connection to `connections.json`. The record holds the
token in `credentials` and the optional configuration fields beside it:

```json
{
  "connectorId": "notion",
  "connectionName": "notion-workspace",
  "configuration": {},
  "credentials": {"api_token": "ntn_…"}
}
```

| Field | Default | Meaning |
| --- | --- | --- |
| `endpoint` | `https://api.notion.com` | API base URL without `/v1`; change it only for a supported gateway or a loopback test server. |
| `maxResponseBytes` | `4194304` | Largest accepted response. An oversized read selects `invalidResponse`; an oversized response to an accepted create selects `uncertain`. |
| `api_token` | | Secret internal connection API token. |

Configuration is read when the application starts; the token is reread before
every call.

## Operations

Every failure names the HTTP status and Notion's machine-readable error code,
such as `validation_error` or `restricted_resource`, and never Notion's
message text. Receipts carry Notion's `x-notion-request-id`. Reads retry
transport failures, 408, 409, 429, 529, and 5xx, waiting for `Retry-After`.
A 429 whose `rate_limit_reason` is `public_api_request_blocked` selects
`providerRejected`, because Notion documents that no retry can help.

### search

Searches page and data source titles, which is all Notion search matches. It
returns one page of `SearchMatch` values, with object type, ID, title, parent,
last edit time, and trash state, plus Notion's cursor. `ObjectType` limits the
search to `page` or `data_source`, and `SortDirection` orders by last edit
time instead of relevance. Notion documents that search is not exhaustive and
indexes new pages with a delay. An empty first page selects `noMatch` instead
of an empty success, so a misspelled title or an unshared database cannot pass
for "nothing to do". An empty continuation page is still `searched`.

### queryDatabase

Queries one page of rows, 1 to 100 per page, from a data source with a typed
`QueryFilter` and `QuerySort`s. It always sends `result_type: page`.
`Properties` limits the returned properties through `filter_properties`. The
output names the resolved `DataSourceID`. `IsResultLimitReached` reports
Notion's 10,000-row limit for one query.

A filter is a property condition, a timestamp condition, or an `and`/`or`
group nested at most two levels, with at most 100 conditions. Each property
type accepts only the conditions Notion documents for it. The condition takes
exactly one value field: `Text`, `Number`, `Checkbox`, `Date`, or `ID`, or
none for `is_empty`, `is_not_empty`, and relative dates such as `past_week`.
The connector checks every combination before it sends anything, and an
unsupported one selects `defect`. Formula, rollup, and verification filters
are not supported.

### getPage

Reads a page and up to `MaxBlocks` top-level blocks, 100 by default and at
most 500, following block cursors. `Content.Text` renders one line per block.
List items start with `- ` or a number, to-do items with `[ ] ` or `[x] `,
table rows join their cells with ` | `, and child pages and databases show
their titles. Blocks inside toggles, columns, and other parents are not read;
`NestedBlockCount` counts the blocks that have them. Text stops at
`MaxTextCharacters`, 20,000 by default, with `IsTextTruncated` set.
`ShouldSkipContent` reads only the properties.

Property values arrive as `PageProperty`: the typed value, the property ID, a
one-line `PlainText`, and `IsTruncated` when Notion returned only the first 25
relation entries. Read-only types fill the closest typed field, such as
`Number` for a numeric formula. A type Notion adds later keeps its name with
an empty value.

### createPage

Creates one row in a data source. `Properties` maps property names, or IDs
such as `title`, to typed `PropertyValue`s built with constructors such as
`TitleValue`, `SelectValue`, and `DateValue`. `BodyText` becomes paragraph
blocks: blank lines separate paragraphs, and the text is never read as markup.
Long text is split into Notion's 2,000-character rich text objects, counted in
UTF-16 code units as Notion counts them.

### updatePageProperties

Sets property values on one page with a `PATCH`. Properties it omits keep
their values. Each listed value replaces the whole property, including every
entry of a multi-select, people, or relation value. `ClearedValue` empties a
property; checkbox and status properties cannot be cleared.

## Avoiding duplicate rows

Notion has no idempotency key or client request ID. Its request-limit guidance
retries only 429 and 529 for writes, and warns that a write can be saved and
still answer 503.

`createPage` therefore never resends a request Notion may have received. It
retries only a request that never reached Notion, a 429, or a 529.

- A conclusive 4xx selects `providerRejected` or `notFound`.
- A 503 whose `additional_data.committed_resource_id` names the saved page
  selects `created`: the connector reads that page back and sets
  `IsConfirmedAfterTimeout`.
- Every other ambiguity selects `uncertain`. That covers a timeout after
  dispatch, a 409, a 5xx, or an interrupted or unusable 2xx.
- The request may wait 60 seconds, longer than Notion's roughly 55-second
  write deadline, so a slow save becomes a confirmed 503 rather than a client
  timeout.
- The operation uses sync durability with a 90-second Execute and heartbeat
  timeout. Under async durability, Dex dispatches a Step again when its local
  attempt passes about seven seconds, and that second dispatch would create a
  second row.

`updatePageProperties` sends absolute values, so a repeated `PATCH` leaves the
page in the same state. It keeps async durability and retries every
ambiguous outcome, including a 503 after Notion saved. A retry can overwrite
an edit someone else made to the same properties in between.

The connector's protection covers one Step execution. Across Flow runs, give
each row a business key and query for it before creating, as the example does
with its Submission ID property:

```go
// MapToFindSubmissionRowsInput reads up to two rows with the submission ID, enough to see a duplicate.
func MapToFindSubmissionRowsInput(record SubmissionRecord) notion.QueryDatabaseInput {
	return notion.QueryDatabaseInput{
		DataSourceID: record.DataSourceID,
		Filter: &notion.QueryFilter{
			Property: SubmissionIDProperty, Type: notion.PropertyTypeRichText, Condition: notion.FilterEquals, Text: record.Submission.SubmissionID,
		},
		Sorts:    []notion.QuerySort{{Timestamp: notion.TimestampCreatedTime, Direction: notion.SortAscending}},
		PageSize: duplicateDetectionPageSize,
	}
}
```

Wire `uncertain` to a reconciliation Step that queries the same key again
rather than creating again:

```go
dex.DefineStep(notion.NewCreatePageStep(notion.CreatePageStepConfig[SubmissionRecord]{
	StepType: createSubmissionRowStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "notion", GroupLabel: "Notion",
		Explanation: "Create the submission row with its properties and the message as the page body.",
	},
	Connection: flow.connection, MapToOperationInput: MapToCreateSubmissionRowInput,
	Created:          sdkgo.GoTo(recordCreatedSubmission{}),
	Uncertain:        sdkgo.GoTo(recordUncertainSubmission{}),
	ProviderRejected: sdkgo.GoTo(recordRejectedCreate{}),
})),
```

A Worker lost while a create is in flight can still repeat it on the next
attempt, because nothing durable records the dispatch. The business-key query
finds such a duplicate on the next run.

## Not in this release

- **OAuth for public connections.** Notion's REST OAuth has no scopes, and its
  token response carries no `scope` string. It documents HTTP Basic client
  authentication with a JSON body, issues refresh tokens, and returns no
  `expires_in`. The manifest schema requires at least one OAuth scope, and Dex
  Web `cli-v1.1.0` requires every declared scope in the token response's
  `scope` string. It also sends a form-encoded body with the client secret in
  the body. A Notion OAuth method therefore cannot complete authorization in
  Dex Web, and the connector ships only the API token method.
- **Pickers.** Studio commands are HTTPS `GET` only. Notion lists databases
  and data sources only through `POST /v1/search`, and `GET /v1/views` needs a
  database or data source ID first. Operations take guided ID input instead.
- **Triggers.** Notion webhooks start with a one-time POST of a
  `verification_token` that the user pastes back into Notion, then sign
  deliveries with `X-Notion-Signature` as an HMAC-SHA256 of the body. The
  shared `webhooktrigger` endpoint has no handshake hook yet, and the events
  carry IDs only, so each would also need a follow-up read.
- Nested block content, Notion's markdown endpoints, formula and rollup
  filters, page property pagination beyond Notion's 25-entry page values,
  trashing pages, and comments.

## Example

[examples/submission-intake](examples/submission-intake) records a form
submission as a database row and reads it back. It finds the database by
title, updates the row that already holds the Submission ID or creates one,
and reconciles an uncertain create by querying again.

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := notion.NewLocalConnection(store, submissionintake.ConnectionName)
if err != nil {
	return err
}
```

## Verification

From the repository root:

```bash
GOWORK=off go run ./cmd/connectorctl validate connectors/notion/connector.yaml
GOWORK=off go run ./cmd/connectorctl generate --check connectors/notion/connector.yaml
GOWORK=off go run ./cmd/connectorctl catalog --check --catalog catalog.yaml
```

From this directory:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1 -v
```

The integration tests need a running `dexcli dev`. They drive the example
through a real Dex Server against a stateful fake Notion. The scenarios are:

- a created row;
- a resubmission that updates the existing row;
- duplicate rows;
- a missing or ambiguous database title;
- a rejected create;
- a rate-limited create retried after `Retry-After`;
- an uncertain create reconciled by query, and one that stays uncertain;
- a create Notion saved before a 503;
- a create the fake holds for nine seconds, which Notion receives once;
- an update the fake holds for nine seconds, where Dex dispatches twice and
  the row converges;
- an unwired optional branch;
- invalid Start Flow input.

`go test -tags=live -run TestLive ./...` exercises every operation against a
real workspace when `NOTION_CONNECTOR_TEST_TOKEN` and
`NOTION_CONNECTOR_TEST_DATA_SOURCE_ID` name an internal connection token and a
disposable data source shared with it. It leaves one created row for manual
cleanup.
