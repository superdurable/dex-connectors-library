# monday.com Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/monday/go@v0.25.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/monday`.

> **Verification status: partial live.** Only an unknown-client probe reached the monday.com OAuth token endpoint; everything else ran on a real Dex stack against a local stand-in. No live monday.com account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The monday.com Connector reads and writes items on monday.com work management
boards from Dex Flows through the monday.com platform API, a GraphQL API at
`https://api.monday.com/v2`. It exposes these operation-specific Dex Step
factories:

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `monday.NewListItemsStep` | Query | async | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `monday.NewGetItemStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `monday.NewCreateItemStep` | Mutation | sync | `created` | `notFound`, `providerRejected`, `uncertain`, `defect` |
| `monday.NewUpdateItemColumnValuesStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `monday.NewAddUpdateStep` | Mutation | sync | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation
uses a 30-second Execute timeout. Reads and the absolute `updateItemColumnValues` use the one-hour default retry budget; `createItem` and `addUpdate` keep ten attempts within five minutes. Every mutation sends a monday.com `Idempotency-Key`. `createItem` and `addUpdate` use sync durability so dispatch checkpoints are visible to later attempts; see [Duplicate safety](#duplicate-safety).

It also exposes one poll Trigger:

| Trigger | Event | Configuration | Delivers |
| --- | --- | --- | --- |
| `itemChanged` | `monday.ItemChangedEvent` | `monday.ItemChangedTriggerConfiguration` (`boardId`) | each active item created or changed on one board |

See [itemChanged Trigger](#itemchanged-trigger). This release has no webhook
Triggers and no Studio pickers; the Trigger's board ID is entered by hand. See
[Not in this release](#not-in-this-release).

## monday.com setup

A connection uses one of two authentication methods. Both send the token in
the `Authorization` header without a `Bearer` prefix, as monday.com's
[authentication guide](https://developer.monday.com/api-reference/docs/authentication)
shows.

### Personal API token (default, recommended)

The token belongs to the user the Flows act as: items and updates the
connector writes are authored by that user, and the user's board, column, and
item permissions limit what it can read and change. In monday.com, select the
profile picture, choose **Developers** to open the Developer Center, then
**API token > Show**. Account admins can also use **Administration >
Connections > Personal API token**. The token has no documented prefix or
expiry; **Regenerate** invalidates it at once, so paste the replacement into
Dex Web. Credentials are reread before every provider call, so a replacement
takes effect without a restart.

### monday.com OAuth 2.1

For a monday.com app, create or open the app in the Developer Center, and
under **OAuth & Permissions** add the scopes `boards:read`, `boards:write`, and
`updates:write` and the Redirect URI that Dex Web shows. Then create a draft
version, enable its **New OAuth Flow** toggle, and promote it, or set it
**Active for me** while testing. Copy the Client ID and Client secret from
**Basic Information > App credentials**.

The connection uses monday.com's
[OAuth 2.1 flow](https://developer.monday.com/apps/docs/migrating-to-the-new-oauth-flow):

- authorization at `https://auth.monday.com/oauth2/authorize` with S256 PKCE,
  which the flow requires and Dex Web sends;
- code exchange and refresh at `https://auth.monday.com/oauth_ms/oauth/token`.

Access tokens are JWTs that expire, and every refresh returns a new refresh
token. monday.com's token response carries no `expires_in`, so
`CredentialRefreshDriver` reads the access token's `exp` claim, without
verifying the token, to schedule a refresh five minutes before expiry. It
refreshes immediately when the claim is unreadable, and records a nominal
one-hour expiry in that case: monday.com's migrate endpoint documents
`expires_in: 3600` for these tokens. The refresh goes through `sdkgo/oauthtoken`
with a JSON body and `client_secret_post`, as monday.com documents. A refresh
that returns a `scope` lacking a required scope, or answers `invalid_grant`,
`invalid_client`, `unauthorized_client`, or `invalid_token`, requires
reauthorization. After monday.com rejects an OAuth token with 401, the
connector asks once for a refresh, which project storage performs only when the
stored expiry has passed, and then resends once; a 401 means monday.com did not
run the request, so the resend cannot duplicate a mutation. Otherwise the 401
selects `providerRejected` with `AUTHENTICATION`.

monday.com's legacy flow, where the app's New OAuth Flow toggle is off, issues
non-expiring tokens from `https://auth.monday.com/oauth2/token` and supports no
refresh; this release does not declare it.

## Project configuration

Dex Web or Superverse Studio saves the connection's settings and credentials in
the project configuration. The application loads that configuration once and
opens the connection by the name it declares, as
[`examples/work-request/main.go`](go/examples/work-request/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := monday.NewProjectConnection(project, workrequest.ConnectionName, connectionOptions()...)
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../sdkgo/projectconfig/README.md).
`NewProjectConnection` reads credentials from project storage for every call.
It leaves a personal API token untouched and refreshes an OAuth access token
five minutes before its recorded expiry, or its `exp` claim when no expiry is
recorded, storing the refreshed token pair atomically.

## API version

Every request sends `API-Version: 2026-10` (`monday.APIVersion`), which
monday.com makes the Current version on October 1, 2026. Its breaking changes
touch only the User entity and Workforms, which the connector does not read.
GraphQL errors use the standard `errors` array introduced in 2025-01. Changing
the pinned version is a connector release.

## Operations

All five operations send one GraphQL document per request with its values in
`variables`, so input never becomes part of the query text. IDs are decimal
strings such as `1234567890`; monday.com's IDs exceed 32 bits.

### listItems

`listItems` reads `boards(ids: [boardId]) { items_page(limit, query_params) }`
and returns one page of the board's active items. `items_page` never returns
archived or deleted items. `limit` is 1 to 100 (`MaxListItemsLimit`), 25 by
default; monday.com allows 500, and the connector keeps Step state smaller.
`nextCursor` continues the listing through the root
`next_items_page(cursor, limit)`. Set only `boardId` and `cursor` for a later
page: monday.com's cursor carries the first page's filter and rejects a new
one. A cursor expires 60 minutes after the first page. `columnIds` limits each
item's column values to at most 50 columns; empty returns every column. A
board that does not exist, or that the connection cannot see, selects
`notFound`.

`filter` is a typed `ItemFilter`: at most 10 `ItemFilterRule`s joined by
`and` (the default) or `or`. Each rule names a `columnId`, an `operator` from
monday.com's `ItemsQueryRuleOperator` (`any_of` by default), and string
`values` or numeric `numbers` in monday.com's per-column format:

| Column | Example rule |
| --- | --- |
| name, the item name | `{columnId: name, values: [Monthly Fire Drill]}`, an exact match |
| status | `{columnId: status, numbers: [1]}` by label index, or `contains_terms` with `values: [Done]` |
| text | `{columnId: text, operator: contains_text, values: [drill]}` |
| date | `{columnId: date4, values: [EXACT, 2026-07-01]}`, `between` two dates, or `values: [TODAY]` |
| people | `{columnId: person, values: [person-48202303]}` |

`is_empty` and `is_not_empty` take no value. `contains_text`,
`not_contains_text`, `contains_terms`, `starts_with`, and `ends_with` take
exactly one string. `between` takes exactly two values. The other operators
take 1 to 50. `order` sorts by one column or by `ItemOrderCreationTime` or
`ItemOrderLastUpdated`. An unsupported combination selects `defect` before any
request.

### getItem

`getItem` reads `items(ids: [itemId], exclude_nonactive: true)`. An empty list
selects `notFound`, so an archived or deleted item reads as `notFound` unless
`includesInactive` is set, which returns it with its `state`.

### Items and column values

An `Item` carries its ID, name, state, board, group, creator, URL, timestamps,
and column values. Each `ItemColumnValue` keeps monday.com's `type`, its
display `text`, and its raw JSON `value`. monday.com returns `value` as a
JSON-encoded string; the connector decodes it to JSON. Text is cut at 4 KiB
(`MaxColumnTextBytes`) on a UTF-8 boundary and flagged `textTruncated`. A raw
value above 16 KiB (`MaxColumnValueBytes`) is omitted and flagged
`valueOmitted`. Status labels and other text are the board's own values,
never mapped to another vocabulary. monday.com does not document the format
of its `Date` scalar, so timestamps that are not RFC 3339 or
`YYYY-MM-DD HH:MM:SS` read as zero.

### createItem

`createItem` sends `create_item(board_id, group_id, item_name, column_values,
create_labels_if_missing)`. `itemName` is 1 to 255 characters on one line.
Blank `groupId` uses the board's first active group. `columnValues` maps up to
50 column IDs to typed `ColumnValue`s, encoded as the one JSON string monday.com
expects:

| Type | Constructor | monday.com JSON |
| --- | --- | --- |
| `text` | `TextValue("Sample")` | `"Sample"` |
| `long_text` | `LongTextValue(...)` | `{"text": ...}`, at most 2,000 characters |
| `numbers` | `NumberValue(42.5)` | `"42.5"` |
| `status` | `StatusLabelValue("Done")`, `StatusIndexValue(1)` | `{"label": ...}` or `{"index": ...}` |
| `date` | `DateValue("2026-02-18")`, `DateTimeValue(date, "09:00:00")` | `{"date": ..., "time": ...}`, time in UTC |
| `people` | `PeopleValue(48202303)` or `PersonIDs` and `TeamIDs` | `{"personsAndTeams": [{"id": ..., "kind": "person"}]}` |
| `checkbox` | `CheckboxValue(true)` | `{"checked": "true"}`; false clears it |
| `email` | `EmailValue(...)` | `{"email": ..., "text": ...}` |
| `link` | `LinkValue(url, text)` | `{"url": ..., "text": ...}` |
| `phone` | `PhoneValue("+12025550169", "US")` | `{"phone": ..., "countryShortName": ...}` |
| `dropdown` | `DropdownLabelsValue(...)` or `DropdownLabelIDs` | `{"labels": [...]}` or `{"ids": [...]}` |
| `timeline` | `TimelineValue(start, end)` | `{"from": ..., "to": ...}` |

`ClearedValue(type)` sends `null`, which empties any of these columns; an empty
numbers string would set 0 instead. A field that does not belong to the type,
or a value that does not match its format, selects `defect`. The name column is
`itemName`, not a column value. Without `createsLabelsIfMissing`, an unknown
status or dropdown label is rejected; with it, monday.com adds the label, which
needs permission to change the board structure. The result is the created
item without column values. That keeps the response far below the 1 MB limit
of monday.com's idempotency replay cache.

### updateItemColumnValues

`updateItemColumnValues` sends `change_multiple_column_values(board_id,
item_id, column_values)` with 1 to 50 typed values, the same encoding as
`createItem`. Columns left out keep their values. Values are absolute: a
people or dropdown value lists every entry the column keeps. The result is
the item with the values of the columns the Step set.

### addUpdate

`addUpdate` sends `create_update(item_id, body)`. An update is monday.com's
comment on an item. The body is plain text of at most 10,000 characters
(`MaxUpdateBodyCharacters`), a connector bound because monday.com documents
none. It is HTML-escaped with line breaks kept as `<br>`, so text is never
read as markup. The result is the update ID, creator, and creation time.

## itemChanged Trigger

`itemChanged` starts a Flow for each active item created or changed on one
board. monday.com webhooks need a challenge handshake and, for a personal
token, cannot be authenticated (see [Triggers](#triggers)), so the Trigger
polls with the connection's existing `boards:read` access: no new scope.

Each poll sends one `items_page` request for the board with
`query_params: {order_by: [{column_id: "__last_updated__", direction: desc}]}`
and 100 items per page with every column value. monday.com documents that
`__last_updated__` sorts items chronologically by last-updated time. The poll
follows `next_items_page` cursors until it reaches an item last updated before
the cursor position minus a five-minute lookback, at most 100 pages. A quiet
board costs one API call per poll.

- **Baseline.** The first poll records the newest last-updated time and the
  items updated in the five minutes before it, and delivers nothing. On an
  empty board it records no time, so every item that later appears is
  delivered.
- **Cursor.** The durable poll cursor holds the board ID and the latest
  delivered last-updated time. Its keyed snapshot holds a content hash for each
  item updated inside the lookback window. An item is delivered when its hash
  differs from the snapshot, so a second change within the same second, or one
  monday.com indexes late, still arrives, and an unchanged item never arrives
  twice.
- **Order and batches.** Changes are delivered oldest first, at most 100 and
  about 4 MiB of item JSON per batch, so a batch always fits the durable
  Trigger inbox, even with a raised `maxResponseBytes`. The rest of a larger burst comes from the same scan, so a burst of
  1,000 changes costs one scan of about 10 pages.
- **Change kind.** `created` is an item the snapshot has not seen whose
  `createdAt` falls inside the lookback window before the previous position, so
  an item created and edited between polls is still `created`. Every other
  change is `updated`.
- **Event ID.** `monday-item-` plus a hash of the board, item ID, and item
  content, so a refetch after a crash yields the same ID and
  `sdkgo.NewDexFlowTriggerTarget` starts one Flow per item version.
- **Reset.** A cursor that does not decode, a cursor for another board, and
  more than 10,000 items changed since the last poll mark the cursor invalid.
  The kit records a new baseline, increments `resetCount`, and logs
  `poll cursor invalid; reseeding` at WARN; changes in that gap are not
  delivered. Changing a binding's board therefore records a new baseline.
- **Rate limits.** HTTP 429 and the documented `RATE_LIMIT_EXCEEDED`,
  `COMPLEXITY_BUDGET_EXHAUSTED`, `IP_RATE_LIMIT_EXCEEDED`,
  `maxConcurrencyExceeded`, and `FIELD_LIMIT_EXCEEDED` codes pause every binding
  of the connection for the longer of `Retry-After` (or `retry_in_seconds`)
  and `pollInterval`. `DAILY_LIMIT_EXCEEDED` pauses it until the daily limit
  resets at midnight UTC. Other failures, such as a board the connection cannot
  see, back off this binding only, up to the kit's maximum.
- **Not delivered.** `items_page` returns active items only, so archiving,
  deleting, or moving an item to another board delivers nothing. Subitems live
  on their own board and are not polled. An item without a readable
  `updated_at` is skipped.

`pollInterval` (at least 30 seconds) sets the delay between polls. Every poll
call counts against the account's
[daily call limit](https://developer.monday.com/api-reference/docs/rate-limits#daily-call-limit):
200 calls on Free, 1,000 on Standard or Basic, 10,000 on Pro, and 25,000 on
Enterprise. The default interval spends 288 calls a day per binding on a quiet
board.

The binding's only Dex Web field is the `boardIdInput` unit, which saves
`boardId`. It accepts the digits after `/boards/` in the board's address or the
whole address. `monday.NewProjectItemTriggerRunner` loads the saved board,
records each change in the binding's project Trigger inbox, and commits the
cursor in the binding's project poll cursor only after recording, as
[`examples/item-changed/main.go`](go/examples/item-changed/main.go) does:

```go
// Each change is recorded in the binding's project Trigger inbox before the poll cursor moves past it.
triggerRunner, err := monday.NewProjectItemTriggerRunner(project, itemchanged.ConnectionName, monday.ProjectItemTriggerRunnerConfig{
	ItemChangedRoutes: []monday.ProjectItemChangedTriggerRoute{{
		BindingName: itemchanged.ItemChangedTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, itemchanged.AcceptEveryItemChange, itemchanged.ResolveFlowID,
			itemchanged.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
	}},
}, monday.WithLogger(logger))
```

A binding without a saved board is an error. `monday.NewItemTriggerRunner`
takes targets and cursor stores directly, and the generated
`monday.NewItemChangedTrigger` keeps the cursor in memory, so it records a new
baseline on every start. `monday.WithLogger` sends the runner's
[poll records](../../sdkgo/README.md#polling-triggers) to a logger; records
never carry item payloads or credentials.

## Duplicate safety

monday.com documents an
[`Idempotency-Key`](https://developer.monday.com/api-reference/docs/idempotency)
header for mutations:

1. The first request with a key runs and its response is cached for 30 minutes.
2. A retry with the same key returns the cached response with
   `Idempotency-Replayed: true`.
3. A concurrent duplicate gets `409 IDEMPOTENCY_CONFLICT` with `Retry-After`.

Every mutation sends the Dex Call ID as its key, stable across attempts of one Step. `createItem` and `addUpdate` record an attempt-numbered dispatch heartbeat before sending. A later attempt resends only when the immediately preceding attempt recorded that it was safe to retry. A missing or stale marker fails closed through `uncertain`; sync Execute durability makes the marker visible to the next attempt. Keep these operations synchronous and leave their heartbeat value to the connector: async durability or another heartbeat writer can make a later attempt fail closed to `uncertain`. The request stops at least three seconds before the 30-second Step deadline, and a write starts only when the remaining request window after that margin is at least three seconds.

The [monday.com idempotency contract](https://developer.monday.com/api-reference/docs/idempotency) caches responses for 30 minutes, returns 409 for concurrent requests with one key, and marks a replay with `Idempotency-Replayed: true`. It also says that responses over 1 MB and responses that exceed the user/app memory budget are **not cached**. Therefore, an accepted but malformed, oversized, interrupted, credential-reflecting, or ID-less 2xx response selects `uncertain`; the connector does not resend it. A transport failure after dispatch with no response also selects `uncertain`; this intentionally gives up the earlier same-key replay because the provider may have applied the write without caching its result. The connector disables Go HTTP transport body replay so it cannot resend an accepted write behind the checkpoint.

A positively unsent request, transient credential-refresh failure, HTTP 408, 409, 423, 429, and every HTTP 5xx response record a can-resend checkpoint and return Retry. An unreadable non-2xx body also does so. A readable 2xx with no mutation data retries when it carries a documented refusal code (`API_TEMPORARILY_BLOCKED`, a rate-limit code, or `IDEMPOTENCY_CONFLICT`) or `status_code` 409, 423, 429, or 503. A 2xx with only another 5xx `status_code` selects `uncertain`, because execution is not ruled out. A 2xx with mutation data never records can-resend. A successful replay sets `replayed: true` and `idempotencyReplayed` in the Receipt. For HTTP 408 and 5xx, the first request may already have executed: monday.com's cache exceptions can make a same-key resend execute again and create a duplicate item or update. The five-minute retry budget limits this residual risk but does not eliminate it.

For `updateItemColumnValues`, that limit is harmless: the values are absolute,
so a repeat changes nothing, and the manifest declares it
`repeatOnUnknown: safe`. `createItem` and `addUpdate` keep the default
`limited` tier with `maxSends: 3` in their definitions; their checkpoint is
stricter, because it never resends a request that may have been sent.

## Errors

monday.com answers application errors with HTTP 200 and an `errors` array,
and transport errors with 4xx or 5xx. A Failure never carries monday.com's
`message` text. It repeats only `extensions.code` and an
`error_data.column_id` token, such as
`monday.com rejected the input (HTTP 200) [ColumnValueException; column status]`.
A token that contains the credential is dropped. A 200 whose `status_code`
extension is 4xx is classified by that status. A documented code decides
before the HTTP status.

| Response | Reads | Mutations |
| --- | --- | --- |
| `COMPLEXITY_BUDGET_EXHAUSTED`, `ComplexityException`, `RATE_LIMIT_EXCEEDED`, `IP_RATE_LIMIT_EXCEEDED`, `maxConcurrencyExceeded`, `FIELD_LIMIT_EXCEEDED`, 429 | Retry after `retry_in_seconds` or `Retry-After` | same |
| `IDEMPOTENCY_CONFLICT`, 409 without a code | Retry after at least 2 seconds | same |
| `API_TEMPORARILY_BLOCKED`, 423 | Retry | same |
| 408, 5xx, connection or read failure | Retry | Retry after 408/5xx or confirmed no-send; lost sent response selects `uncertain` for create/add |
| `DAILY_LIMIT_EXCEEDED` | `providerRejected`, `QUOTA_EXHAUSTED` | same |
| `Unauthorized`, `NOT_AUTHENTICATED`, 401 | `providerRejected`, `AUTHENTICATION`; an HTTP 401 for an OAuth token whose stored expiry has passed is refreshed and resent once first | same |
| `UserUnauthorizedException`, `USER_ACCESS_DENIED`, `missingRequiredPermissions`, 403 | `providerRejected`, `AUTHORIZATION` | same |
| `ResourceNotFoundException`, `InvalidBoardIdException`, `InvalidItemIdException`, 404 | `notFound` | `notFound`; nothing was written |
| `ColumnValueException`, `CorrectedValueException`, `InvalidColumnIdException`, `InvalidArgumentException`, `ItemNameTooLongException`, `ItemsLimitationException`, `RecordInvalidException`, 422 | `providerRejected`, `VALIDATION` | same |
| GraphQL parse or validation errors, `JsonParseException`, `InvalidVersionException`, 400 | `defect`, `PROTOCOL` | same |
| any other code or status, 3xx | `providerRejected`; redirects are never followed | same |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` | `uncertain`, or `invalidResponse` for `updateItemColumnValues` |
| invalid input or connection credentials | `defect`, with no request | same |

A mutation whose field returned a usable object beside a nested field error
selects its happy branch, because the write happened. monday.com documents
that an invalid board or item ID can also surface as a 500, which is retried
until the window ends. The Receipt carries the Call ID, the idempotency key,
the board, item, or update ID, and monday.com's `extensions.request_id`.

## Mocks

The generated `mondaymock` package provides a consistent February work-request world: three completed-item search pages, item `5550004`, and update `3300001`. Every operation has a happy-path default and mocks for definite `notFound`, `providerRejected`, and read/absolute-update `invalidResponse` outcomes. The runnable example uses the defaults in a real Dex integration test:

```go
mock := mondaymock.New(t, ConnectionName)
harness := newWorkHarnessForConnection(t, mock.Connection())
outcome := harness.runWorkRequest(t, "mock-default", integrationInput())
require.Equal(t, WorkItemCreated, outcome.Action)
require.Equal(t, "5550004", outcome.ItemID)
require.Equal(t, "february", outcome.GroupID)
require.Equal(t, "3300001", outcome.UpdateID)
```

The test also asserts three recorded list inputs, one create input, one update input, and no get or schedule calls. Mock answers are fixtures, not live monday.com verification.

## Not in this release

### Studio pickers

A board or group picker needs a Studio setup command, and Studio commands
support only HTTPS `GET` to a fixed host that returns a JSON object. monday.com
removed GraphQL over `GET` in API version 2025-01; queries must be in a `POST`
body. It has no REST endpoint that lists boards or groups. Board, group, and
column IDs are therefore Flow input, and the `itemChanged` binding's board ID is
entered in the `boardIdInput` unit, which ships in the release-owned Studio
bundle under `ui/` and makes no provider request. A board's ID is the number
after `/boards/` in its URL. With **Developer mode**, enabled from the profile
picture > **monday.labs**, monday.com shows column IDs in the board. `getItem`
and `listItems` return each item's group ID.

### Triggers

monday.com webhooks, created with `create_webhook`, verify the receiving URL by
POSTing a `challenge` value that the receiver must echo in its JSON response.
`sdkgo/webhooktrigger` has no handshake hook yet. Only webhooks created with an
app token carry a JWT `Authorization` header, so a personal-token webhook
cannot be authenticated. Until both exist, the polling
[`itemChanged`](#itemchanged-trigger) Trigger is the only Trigger; update,
subitem, archive, and delete events are not delivered.

### Conflict detection

monday.com documents no conditional update, so `updateItemColumnValues`
overwrites a concurrent change to the columns it sets.

## Examples

[`examples/item-changed`](go/examples/item-changed) starts one
`MondayItemChange` Flow per created or changed item on the board saved in its
`board-item-changes` binding, reads the item back with `getItem`, and records
it.

[`examples/work-request`](go/examples/work-request) is a runnable Dex Web
**Start Flow** example that uses all five operations. It finds a board's open
item for a recurring work request by exact name, reads it back, and sets its
status and due date. Otherwise it creates the item. In both cases it adds one
update.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example owns its real
Worker, retry, persistence, duplicate-dispatch, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/monday/connector.yaml
go run ./cmd/connectorctl generate --check connectors/monday/connector.yaml
```

The provider fakes cover:

- the raw token header, `API-Version`, and POST-only GraphQL with variables;
- the `Idempotency-Key` on mutations only and its stability across retries;
- typed filter and column-value encoding and the cursor continuation;
- JSON-encoded column values and bounded text and values;
- every error-code mapping without message text, including `retry_in_seconds`
  and `Retry-After`;
- partial data beside a nested error, redirects, and oversized, malformed, and
  credential-reflecting responses;
- the OAuth 2.1 refresh with JWT expiry, scope checks, and a single refresh
  and resend after a 401 through a refreshing credential source;
- for `itemChanged`, the `__last_updated__` descending order and cursor
  pagination, the baseline, same-second changes, a burst split into batches
  from one scan, 429 with `Retry-After`, `DAILY_LIMIT_EXCEEDED`, and the reset
  after more than 10,000 changes, in unit tests and in real Dex integration
  tests of the example across a Trigger process restart.

No live monday.com account was used. The provider's cache-budget exception, checkpoint recovery, and exact 503 behavior were verified only against documentation and a fake provider. The following live behavior is
unverified:

- whether `items(ids:)` returns an empty list for a missing or deleted item,
  and which code a missing group returns;
- the exact Retry-After value monday.com sends with `IDEMPOTENCY_CONFLICT`, and
  whether a cached response is replayed for an errored mutation;
- whether the HTTP status of `DAILY_LIMIT_EXCEEDED` is 429;
- the format of the `Date` scalar;
- the body length limit of `create_update`;
- whether the OAuth 2.1 token endpoint accepts Dex Web's form-encoded code
  exchange beyond client validation (an unauthenticated probe with an unknown
  client returned `invalid_client`, the same as a JSON request);
- whether the token response always omits `expires_in` and the access token
  is always a JWT with `exp`;
- the Developer Center and Developer mode paths in the setup guidance;
- for `itemChanged`: that `__last_updated__` ordering is global across groups
  rather than per group, which the scan's early stop relies on; which edits move
  an item's `updated_at` (for example an update post or a subitem change); and
  the precision of `updated_at`.

## Python package

`python/` provides `dex-connector-monday` v0.2.0, on `dex-connectors-sdk>=0.12.0`, with all five Go operations, both manifest authentication methods, and the `itemChanged` Trigger. `new_project_connection` uses the generated Python OAuth refresh wiring and the static connection name shown in the [runnable work-request example](python/examples/work_request/README.md). The example reads a board page, creates an item, sets typed column values, adds an update, and reads the item back. Provider replay fixtures in `conformance/connectors/monday/scenarios.json` check exact Go request and outcome agreement; live monday.com authorization and API behavior are not verified by those fixtures.

`new_project_item_trigger_runner` polls `itemChanged` with the Python Connector SDK's poll Trigger kit at behavior parity with Go: the same scan, lookback, batches, rate-limit pauses, and reset rules, and cursor documents and Trigger event IDs that match Go byte for byte, so Go and Python replicas share one binding's cursor and inbox. The [item-changed example](python/examples/item_changed/README.md) starts one `MondayItemChangePython` Flow per change.

## TypeScript package

`@superdurable/connector-monday` v0.2.0, on `@superdurable/connectors-sdk` 0.12.0,
implements all five Go operations and the `itemChanged` Trigger:
`listItems`, `getItem`, `createItem`, `updateItemColumnValues`, and `addUpdate`.
Its generated factories, definitions, models, mock cases, and OAuth credential
refresh declarations come from `connector.yaml`. The hand-written provider
client keeps credentials outside Flow state and sends the same GraphQL documents,
API version, and idempotency keys as Go. See the runnable
[TypeScript list-items example](typescript/examples/list-items/README.md) for
Worker setup, the static `monday-board` connection, and a real Dex integration
check. Provider requests can be replayed against the Go-recorded scenarios in
`conformance/connectors/monday/scenarios.json`.

`newProjectItemTriggerRunner` polls `itemChanged` with the TypeScript Connector
SDK's poll Trigger kit at behavior parity with Go: the same scan, lookback,
batches, rate-limit pauses, and reset rules, and cursor documents and Trigger
event IDs that match Go byte for byte, so Go and TypeScript replicas share one
binding's cursor and inbox. The
[TypeScript item-changed example](typescript/examples/item-changed/README.md)
starts one `MondayItemChangeTypeScript` Flow per change.
