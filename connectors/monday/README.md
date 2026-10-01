# monday.com Connector

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
| `monday.NewCreateItemStep` | Mutation | async | `created` | `notFound`, `providerRejected`, `uncertain`, `defect` |
| `monday.NewUpdateItemColumnValuesStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `monday.NewAddUpdateStep` | Mutation | async | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation
uses a 30-second Execute timeout and a five-minute retry window, which fits
monday.com's one-minute complexity budget. Reads allow six attempts and
mutations ten. Every mutation sends a monday.com `Idempotency-Key`, so all
three stay async; see [Duplicate safety](#duplicate-safety).

This release has no Triggers and no Studio pickers; see
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
connector forces one refresh and resends once; a 401 means monday.com did not
run the request, so the resend cannot duplicate a mutation.

monday.com's legacy flow, where the app's New OAuth Flow toggle is off, issues
non-expiring tokens from `https://auth.monday.com/oauth2/token` and supports no
refresh; this release does not declare it.

## Local configuration

Dex Web writes this record for the connection name the application uses:

```json
{
  "connectorId": "monday",
  "modulePath": "github.com/superdurable/dex-connectors-library/connectors/monday",
  "moduleVersion": "v0.1.0",
  "provider": "monday",
  "connectionName": "monday-workspace",
  "configuration": {},
  "credentials": {"auth_method": "personal-api-token", "api_token": "..."}
}
```

Load it with `localconfig.LoadFromEnvironment` and `monday.NewLocalConnection`,
as [`examples/work-request/main.go`](examples/work-request/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := monday.NewLocalConnection(store, workrequest.ConnectionName, connectionOptions()...)
```

`NewLocalConnection` uses the refreshing local provider, which leaves a
personal API token untouched and stores each refreshed OAuth token pair
atomically.

## Hosted credentials

In Superverse-hosted deployments, construct the client with the
operation-scoped broker provider. `DecodeResolvedCredentialsJSON` accepts
`auth_method` with `api_token` for a personal API token, or with
`access_token` for OAuth. It rejects renewal material, the other method's
token, and unknown fields without repeating any value.

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

## Duplicate safety

monday.com documents an
[`Idempotency-Key`](https://developer.monday.com/api-reference/docs/idempotency)
header for mutations:

1. The first request with a key runs and its response is cached for 30 minutes.
2. A retry with the same key returns the cached response with
   `Idempotency-Replayed: true`.
3. A concurrent duplicate gets `409 IDEMPOTENCY_CONFLICT` with `Retry-After`.

Every mutation sends the Dex Call ID as its key: a UUID that is stable for
every attempt of one Step execution, including one on a replacement Worker,
and different for every other Step execution. This lets each mutation keep
async durability:

- When Dex re-dispatches a create or update past its seven-second local
  phase, or retries after a lost Worker, the new attempt gets 409 while the
  first still runs, or monday.com's cached result once it finished. The
  conflict is retried after at least two seconds. Mutations allow ten
  attempts, so a re-dispatched attempt can wait out a slow first one.
- A 5xx, a 408, a lost or unreadable response, and a transport failure are
  retried under the same key. A rate limit, a board lock, and
  `API_TEMPORARILY_BLOCKED` are retried after monday.com's delay; monday.com
  documents that a rate-limited request was not processed.
- `createItem` and `addUpdate` select `uncertain` only when monday.com accepted
  the mutation but its answer is unusable: malformed, oversized, or reflecting
  the credential. A retry would replay the same answer, and the item or update
  may exist. `updateItemColumnValues` selects `invalidResponse` there, because
  repeating it is safe.
- A result replayed from the cache sets `replayed: true` on the output and
  `idempotencyReplayed` in the Receipt metadata.

The guarantee has the limits monday.com documents, and they bound it:

- A cached response expires after 30 minutes. Every retry window is five
  minutes; keep any `StepOptionsOverride` window below 30 minutes.
- A response larger than 1 MB is not cached. The mutations select only
  scalar fields, and the update's column values only for the columns it set.
- Each user and app combination has a memory budget for cached responses.
  monday.com documents that a request beyond the budget runs but is not cached
  for replay, and no response signals it. A re-dispatch after such a request
  would run again. The connector cannot detect this, so a duplicate item or
  update remains possible only when monday.com both lacks cache budget and an
  attempt is repeated.

For `updateItemColumnValues`, that limit is harmless: the values are absolute,
so a repeat changes nothing.

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
| 408, 5xx, connection or read failure | Retry | Retry under the same key |
| `DAILY_LIMIT_EXCEEDED` | `providerRejected`, `QUOTA_EXHAUSTED` | same |
| `Unauthorized`, `NOT_AUTHENTICATED`, 401 | `providerRejected`, `AUTHENTICATION`, after one OAuth refresh | same |
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

## Not in this release

### Studio pickers

A board or group picker needs a Studio setup command, and Studio commands
support only HTTPS `GET` to a fixed host that returns a JSON object. monday.com
removed GraphQL over `GET` in API version 2025-01; queries must be in a `POST`
body. It has no REST endpoint that lists boards or groups. Board, group, and
column IDs are therefore Flow input. A board's ID is the number after
`/boards/` in its URL. With **Developer mode**, enabled from the profile
picture > **monday.labs**, monday.com shows column IDs in the board. `getItem`
and `listItems` return each item's group ID.

### Triggers

monday.com webhooks, created with `create_webhook`, verify the receiving URL by
POSTing a `challenge` value that the receiver must echo in its JSON response.
`sdkgo/webhooktrigger` has no handshake hook yet. Only webhooks created with an
app token carry a JWT `Authorization` header, so a personal-token webhook
cannot be authenticated. Until both exist, poll with `listItems` and an
`order` on `ItemOrderLastUpdated`.

### Conflict detection

monday.com documents no conditional update, so `updateItemColumnValues`
overwrites a concurrent change to the columns it sets.

## Example

[`examples/work-request`](examples/work-request) is a runnable Dex Web
**Start Flow** example that uses all five operations. It finds a board's open
item for a recurring work request by exact name, reads it back, and sets its
status and due date. Otherwise it creates the item. In both cases it adds one
update.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the pinned Dex development server running, the example owns its real
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
  and resend after a 401 through the real local refreshing provider.

No live monday.com account was used. The following live behavior is
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
- the Developer Center and Developer mode paths in the setup guidance.
