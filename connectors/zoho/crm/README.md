# Zoho CRM Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Zoho CRM; no live Zoho CRM account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Zoho CRM Connector finds, reads, upserts, and updates Zoho CRM records,
such as contacts, accounts, and deals, through the Zoho CRM API v8 in the Zoho
data center where the account lives. It is the system-of-record write path for
a Flow: identify a person by email, link them to their account, and move their
deal forward. It exposes these operation-specific Dex Step factories:

| Operation | Kind | Zoho CRM endpoint | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `crm.NewFindRecordsStep` | Query | `POST /crm/v8/coql` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `crm.NewGetRecordStep` | Query | `GET /crm/v8/{module}/{id}` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `crm.NewListModifiedRecordsStep` | Query | `POST /crm/v8/coql` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `crm.NewListModuleFieldsStep` | Query | `GET /crm/v8/settings/fields?module=` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `crm.NewUpsertRecordStep` | Mutation | `POST /crm/v8/{module}/upsert` | `upserted` | `conflict`, `recordRejected`, `providerRejected`, `invalidResponse`, `defect` |
| `crm.NewUpdateRecordStep` | Mutation | `PUT /crm/v8/{module}/{id}` | `updated` | `notFound`, `conflict`, `recordRejected`, `providerRejected`, `invalidResponse`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation is
safe to repeat, so every Step uses async durability, a 30-second Execute
timeout, and six attempts within five minutes, and keeps all of its requests,
including a token refresh, inside a 25-second deadline: each Zoho CRM request
times out after 10 seconds and each token request after 8, unless
`WithHTTPClient` supplies a client with its own `Timeout`. There are no
`uncertain` branches; see [Duplicate safety](#duplicate-safety). The connector
never deletes a record and requests no delete scope.

The operation, branch, and record shapes follow the Salesforce and HubSpot
connectors so a process can move between CRMs by changing vendor values:
`getRecord`, `updateRecord`, and the `found`, `notFound`, `upserted`,
`updated`, `conflict`, `recordRejected`, and `providerRejected` branches carry
the same names, and a `Record` is a module, an ID, and each field's exact JSON
value by API name, as Salesforce's `Record` is an sObject type, an ID, and its
fields. Module names, field API names, stages, and every other picklist value
are Zoho CRM's own and are never mapped to another vocabulary.

There are no Triggers and no Studio units; see
[Not in this release](#not-in-this-release).

## Data centers

Zoho keeps each account in one data center, and both the OAuth server and the
Zoho CRM API host belong to it. A manifest declares static OAuth endpoints, so
the connector declares one authorization method per data center. Dex Web lists
them under **Data center**; choose the one whose Zoho CRM address you sign in
to:

| Method ID | Data center | Zoho Accounts (OAuth) | Zoho CRM API (production) |
| --- | --- | --- | --- |
| `zoho-us-oauth` (default) | United States | `https://accounts.zoho.com` | `https://www.zohoapis.com` |
| `zoho-eu-oauth` | European Union | `https://accounts.zoho.eu` | `https://www.zohoapis.eu` |
| `zoho-in-oauth` | India | `https://accounts.zoho.in` | `https://www.zohoapis.in` |
| `zoho-au-oauth` | Australia | `https://accounts.zoho.com.au` | `https://www.zohoapis.com.au` |
| `zoho-jp-oauth` | Japan | `https://accounts.zoho.jp` | `https://www.zohoapis.jp` |
| `zoho-ca-oauth` | Canada | `https://accounts.zohocloud.ca` | `https://www.zohoapis.ca` |
| `zoho-sa-oauth` | Saudi Arabia | `https://accounts.zoho.sa` | `https://www.zohoapis.sa` |

These are the data centers for which Zoho's CRM v8 SDK names a zohoapis host
and `https://accounts.zoho.com/oauth/serverinfo` lists an Accounts server.
China (`zohoapis.com.cn`) is a separately operated service that `serverinfo`
does not list, as the Zoho Desk connector also decided; Singapore, the United
Arab Emirates, and the United Kingdom have an Accounts server but no
documented Zoho CRM API host. `crm.DataCenters()` returns the table.

Unlike Zoho Desk, Zoho CRM is served from the `api_domain` that Zoho's token
response names, and that host also tells a production organization from a
sandbox (`https://sandbox.zohoapis.<domain>`) or a developer edition
(`https://developer.zohoapis.<domain>`). The manifest maps `api_domain` from
Zoho's token response into the connection's credentials at authorization, as
the Salesforce connector maps `instance_url`, and every refresh stores the
returned value. Before any request the connector accepts it only when it is
exactly the `www`, `sandbox`, or `developer` zohoapis host of the method's data
center, so a token is never sent to another host; any other value selects
`defect` with no request, and a refresh that returns one is retried and never
stored. A connection without `api_domain` refreshes once to obtain it.

## Zoho setup

One Zoho API console client serves every Dex connection. In the API console of
the data center, such as `https://api-console.zoho.eu`:

1. Choose **GET STARTED** or **ADD CLIENT** and create a **Server-based
   Applications** client with a Client Name and Homepage URL.
2. Add the Redirect URI that Dex Web shows under **Authorized Redirect URIs**
   and choose **CREATE**. A client registered in another data center needs this
   data center turned on in its **Settings** tab; the client ID is shared, and
   each data center can have its own client secret.
3. Copy the **Client ID** and this data center's **Client Secret** from the
   **Client Secret** tab into Dex Web.
4. Choose **Connect** and sign in as the Zoho CRM user the connector acts as.
   Records it writes are created and modified by that user, and the user's
   profile and role limit which modules, fields, and records it reads and
   changes. On the consent screen accept every scope; if the account belongs
   to several organizations or a sandbox, pick the one the connection uses,
   because Zoho binds the token to it.

Dex requests `access_type=offline` and `prompt=consent`, so Zoho returns a
refresh token, and these scopes:

| Scope | Used by |
| --- | --- |
| `ZohoCRM.modules.READ` | `getRecord`, and with COQL `findRecords` and `listModifiedRecords` |
| `ZohoCRM.modules.CREATE` | `upsertRecord`, which Zoho documents as accepting CREATE, WRITE, or ALL |
| `ZohoCRM.modules.UPDATE` | `updateRecord` |
| `ZohoCRM.coql.READ` | `findRecords`, `listModifiedRecords` |
| `ZohoCRM.settings.fields.READ` | `listModuleFields` |

The group scopes cover every module, including custom modules; Zoho also
offers per-module scopes such as `ZohoCRM.modules.contacts.READ`, which the
connector does not request because a Flow chooses its modules. PKCE is off,
because Zoho documents none for server-based clients. To revoke access, remove
the connected app from the Zoho account or delete the client; every operation
then selects `providerRejected` with an `AUTHENTICATION` failure until the
connection is reconnected.

## Project configuration

Dex Web or Superverse Studio saves the connection's data center, credentials,
and `api_domain` in the project configuration. The application loads that
configuration once and opens the connection by the name it declares, as
[`examples/lead-qualification/main.go`](examples/lead-qualification/main.go)
does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := crm.NewProjectConnection(project, leadqualification.ConnectionName, connectionOptions()...)
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../../sdkgo/projectconfig/README.md). Credentials
are read from project storage during every provider call and refreshed first
when the access token or `api_domain` is missing, the token has no recorded
expiry, or it expires within five minutes; `maxResponseBytes` is startup
configuration, and the only connection field a user sets besides the OAuth
client.

## Token refresh

Zoho access tokens last one hour. `CredentialRefreshDriver` refreshes them
through `sdkgo/oauthtoken` at the method's `{accounts server}/oauth/v2/token`,
with a form body and the client secret in the body, and retains `api_domain`
from the response. Zoho does not rotate refresh tokens, so the stored one is
kept, and the recorded expiry never exceeds the documented hour. Zoho answers
grant errors with HTTP 200 and an `error` code; `invalid_code` (a revoked
refresh token), `invalid_client` (including a client not enabled in this data
center), and `invalid_client_secret` require reauthorization, and every other
failure, such as Zoho's `Access Denied` token throttle, is retried.

When Zoho CRM answers 401 with any code but `OAUTH_SCOPE_MISMATCH`, the
connector asks once for a refresh. Project storage refreshes only when the
recorded expiry has passed, and the connector then sends the request once more,
with the refreshed `api_domain`, because Zoho CRM rejects an invalid token
before acting on the request. A scope mismatch is not refreshed: Zoho reports
it as a 401 too, and it selects `providerRejected` with an `AUTHORIZATION`
failure that names the scope the operation needs.

## Records, associations, and Zoho's own values

Every operation names a module by its API name, such as `Contacts`,
`Accounts`, `Deals`, `Leads`, or a custom module's name from **Setup >
Developer Hub > APIs and SDKs > API Names**, and fields by API name, such as
`Email`, `Account_Name`, or `Stage`. `crm.ModuleContacts` and `crm.FieldStage`
are constants for the standard names the examples use; module and field names
are 1 to 100 letters, digits, and underscores starting with a letter, and
record IDs are decimal strings of up to 20 digits.

A `Record` holds `Module`, `ID`, and `Fields`, each field's exact JSON value,
with Zoho's `$` keys such as `$approval` dropped. `StringField`, `LookupID`,
`ModifiedAt`, and `DecodeField` read one field. Associations are lookup
fields: a contact's `Account_Name` and a deal's `Account_Name` and
`Contact_Name` hold `{"name": ..., "id": ...}` in a read, and `LookupID`
returns the ID. Writes take `map[string]json.RawMessage`; `TextFieldValue`
encodes text and `LookupFieldValue(id)` writes `{"id": "<id>"}`, which links a
contact to its account or assigns an owner. A record's owner is the `Owner`
user lookup.

Stages and other picklist values pass through unchanged. `listModuleFields`
returns each picklist's `actualValue` options, so a Flow can check a stage
before writing it, as the example does; Zoho CRM rejects an unknown value with
`INVALID_DATA`, which selects `recordRejected`. Whether a stage is closed is
an organization setting the API does not expose with the stage name, so the
example lists Zoho's default closed stages itself.

## Operations

### findRecords

`findRecords` finds up to 200 records of one module that match every
condition, through a COQL query, and is the identity-resolution operation:
a contact by `Email`, an account by `Account_Name`, or the open deals whose
`Contact_Name` is a contact. COQL reads committed records, whereas Zoho's
Search API answers `204` for records written moments earlier and its `equals`
also matches look-alike values, so the connector does not use the Search API.
`BuildFindRecordsQuery` returns the statement for review:

```text
select Email, Account_Name from Contacts where Email = 'jane@acme.example.com' order by id asc limit 0, 10
```

- `Fields` names 1 to 50 field API names or lookup paths with at most two
  joins, such as `Account_Name.Account_Name`; the record ID is always returned,
  and COQL returns only the ID of a lookup.
- `Conditions` holds 1 to 10 `RecordCondition` values that every record
  matches, nested in pairs as Zoho's examples are, `((A and B) and C)`.
  Operators are `=`, `!=`, `>`, `>=`, `<`, `<=`, `in`, `not in`, `is null`,
  and `is not null`, and values are typed: `COQLText`, `COQLTextList`,
  `COQLNumber` (a plain decimal), `COQLBoolean`, `COQLDate`, `COQLDateTime`
  (written with a `+00:00` offset), `COQLRecordID`, and `COQLRecordIDList`.
  Lists hold 1 to 100 entries. A value is written as a literal and can never
  change the query: text with an apostrophe, a backslash, or a control
  character is rejected as `defect` with no request, because Zoho documents no
  COQL escape for them.
- `Sort` orders by one field and then by record ID; nil orders by ID.
- `Limit` is 1 to 200, 10 by default. `NextOffset` is set when Zoho reports
  `more_records`; pass it as the next `Offset`. Offset paging reaches the
  100,000th match, COQL's limit for one set of conditions.

`found` means at least one record matched; `notFound` means none did, so an
empty lookup is never a silent success. Each page costs one API credit.

### getRecord

`getRecord` reads one record with every field, or with up to 50 named fields.
A `204`, an empty data list, or `INVALID_DATA` about the `id` selects
`notFound`. Zoho does not document the answer for a missing or deleted record,
so the connector accepts each of these.

### listModifiedRecords

`listModifiedRecords` is the `changed_since` read: one page of up to 200
records of a module, oldest change first, ordered by `Modified_Time` and then
record ID. Start a feed with `ModifiedSince`, which matches records modified at
or after that second, or continue one with the `Cursor` an earlier page
returned. The cursor is a watermark of the last record's modification time
and ID, such as `2026-01-28T13:00:05Z/4150868000003194012`, and the next page
asks for records strictly after it:

```text
select Deal_Name, Modified_Time from Deals where (Modified_Time > '2026-01-28T13:00:05+00:00' or (Modified_Time = '2026-01-28T13:00:05+00:00' and id > 4150868000003194012)) order by Modified_Time asc, id asc limit 0, 100
```

Keyset paging continues through records that share a second and never skips a
record that another user changes while the feed is read: the change moves it
to the end of the feed, where it appears again. An empty page keeps the
cursor, so a Flow stores the last cursor and passes it to its next poll; it is
not bound to a user or a 24-hour expiry as Zoho's `page_token` is. `HasMore`
reports that Zoho holds further changes now. A page whose records are out of
order, before the cursor, or without `Modified_Time` selects `invalidResponse`.
Deleted records do not appear; Zoho lists them separately.

### listModuleFields

`listModuleFields` reads `GET /crm/v8/settings/fields?module={module}` and
returns each field's API name, display label, Zoho data type, custom,
read-only, system-mandatory, and unique flags, length, lookup module, and
picklist values with `isUnused` for options Zoho keeps but no longer offers.
`FieldNamed` and `HasPicklistValue` read them. `isUnique` tells a Flow whether
a duplicate-check field is marked **Do not allow duplicate values**; see
[Duplicate safety](#duplicate-safety).

### upsertRecord

`upsertRecord` sends one record to `POST /crm/v8/{module}/upsert` with
`duplicate_check_fields`. Zoho CRM checks the named fields in order and
updates the record that holds the value, or inserts a new record when none
does. `DuplicateCheckFields` names 1 to 3 fields, and each must be set in
`Fields` to a non-empty value, or every attempt would insert another record.
Zoho's system duplicate-check fields are `Email` for Leads and Contacts,
`Account_Name` for Accounts, `Deal_Name` for Deals, and `Name` for custom
modules; a field marked **Do not allow duplicate values** also qualifies.
Deal names are not unique in practice, so upsert deals by a unique external ID
field rather than by `Deal_Name`. The result reports the record ID,
`isCreated` for the answered request, the `duplicateField` that matched, and
the record's times.

`Triggers` limits the automations Zoho runs after the write to `workflow`,
`approval`, or `blueprint`; empty keeps Zoho's default, which runs all three.
`ShouldSkipAutomation` sends `"trigger": []`, so none runs. The record ID and
keys starting with `$` cannot be written.

### updateRecord

`updateRecord` sends `PUT /crm/v8/{module}/{id}` with 1 to 200 fields, such as
a deal's `Stage` and `Owner`; fields it does not name keep their values and
JSON `null` clears one. `INVALID_DATA` about the `id` selects `notFound`, and a
blueprint or approval lock selects `recordRejected` with `RECORD_LOCKED` or
Zoho's other code. This release sends no `If-Unmodified-Since`: a retry after
an attempt that already applied would see `ALREADY_MODIFIED` and report a
false conflict, so a change another user makes in between to the same fields
is overwritten, as with any last-write-wins retry.

## Duplicate safety

Zoho CRM documents no idempotency key. Dex re-dispatches an async Step whose
local attempt passes about seven seconds, and retries a Step after a lost
Worker, so every operation is built to be repeated:

- **upsertRecord** converges on one record: a repeated attempt finds the record
  an earlier attempt inserted by its duplicate-check value and updates it. If
  two attempts race and the duplicate-check field is unique, Zoho rejects the
  second insert with `DUPLICATE_DATA`; the connector resends that upsert once,
  which then updates the record, and a second `DUPLICATE_DATA` selects
  `conflict` with the field and the other record's ID. Mark the duplicate-check
  field unique when two attempts may run at once; `listModuleFields` reports
  `isUnique`. Whether Zoho serializes concurrent upserts on a field that is not
  unique, such as a contact's `Email`, is not documented.
- **updateRecord** sets absolute values, so a repeat writes the same values.
- Every unconfirmed outcome is retried: a 5xx, a 408, a 429, a dropped
  connection, or an unreadable response. Zoho runs the selected automations
  again on each repeated write.

`TestSlowContactUpsertDispatchedTwiceKeepsOneContactWithRealDex` proves the
upsert on a real Dex Server: the fake applies the first contact upsert and
answers nine seconds later, Dex dispatches the Step again, and the second
upsert updates the contact, so one contact exists.
`TestSlowDealUpdateDispatchedTwiceSetsTheSameValuesWithRealDex` proves the
update the same way.

## Errors

A Failure never repeats Zoho CRM's `message` text, which can repeat record
values. It names only Zoho's `code`, the field from `details.api_name`, and a
duplicate record's ID, such as
`Zoho CRM rejected the request (HTTP 400) [MANDATORY_NOT_FOUND; field: Last_Name]`.
The write Results carry the same values as `ProviderErrors`. A body that
contains the access token is dropped.

| Response | Reads | upsertRecord and updateRecord |
| --- | --- | --- |
| 400 or a 2xx record-level error such as `MANDATORY_NOT_FOUND`, `INVALID_DATA`, `RECORD_LOCKED`, `DEPENDENT_MISMATCH` | `providerRejected`, `VALIDATION` | `recordRejected`, `VALIDATION` |
| `DUPLICATE_DATA` | `providerRejected` | `conflict`, `CONFLICT`, after one resend for `upsertRecord` |
| `INVALID_DATA` about the `id` | `notFound` on `getRecord` | `notFound` on `updateRecord` |
| 204 | `notFound` on `findRecords` and `getRecord`; an empty page on `listModifiedRecords` | not expected |
| `INVALID_MODULE`, `NOT_SUPPORTED`, `INVALID_URL_PATTERN`, `INVALID_REQUEST_METHOD`, 404 | `providerRejected` | `providerRejected` |
| COQL `SYNTAX_ERROR`, `INVALID_QUERY`, `LIMIT_EXCEEDED` | `providerRejected`, `VALIDATION` | not used |
| 401 (`INVALID_TOKEN`), after one refresh and resend only when the stored token has expired | `providerRejected`, `AUTHENTICATION` | same |
| `OAUTH_SCOPE_MISMATCH` (401), `NO_PERMISSION` (403), `AUTHORIZATION_FAILED` | `providerRejected`, `AUTHORIZATION` | same |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed | same |
| 429 | Retry, after `Retry-After` when present | same |
| 408, 5xx, a dropped connection or unreadable response | Retry | same |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` | `invalidResponse` |
| revoked authorization (`invalid_code` on refresh) | `providerRejected`, `AUTHENTICATION` | same |
| token refresh outage | Retry | same |
| invalid input, an unknown data center, a foreign `api_domain`, or an unusable token | `defect`, with no request | same |

Zoho CRM meters API credits per rolling 24 hours by edition and limits
concurrent calls per organization and app; it answers 429 for either. Zoho
documents no `Retry-After` for them, so a 429 is retried under the Step policy,
and a delay that does not fit the five-minute retry window fails the Step. A
COQL page of up to 200 records and every other call cost one credit; an upsert
or update of one record costs one. The Receipt carries the Call ID, the record
ID, and `X-API-CREDITS-REMAINING` as `remainingCredits` when Zoho sends it,
which it does once half of the day's credits are used.

## Not in this release

### Triggers

Zoho CRM notifications are not Triggers yet:

- A channel is created with `POST /crm/v8/actions/watch` and expires after at
  most one week (one hour by default), so it must be renewed on a schedule.
  `webhooktrigger` owns no subscription lifecycle or renewal timer.
- A callback carries the module, the record `ids`, the `operation`, the
  `channel_id`, `server_time`, and the `token` given at subscription, but no
  provider event ID. A Trigger needs a stable event ID for delivery
  deduplication, and Zoho documents neither retries nor acknowledgement.
- The `token` is a shared value echoed in the body, not a signature over it,
  so it proves the sender only as far as the value stays secret.

Until a Trigger can own channel renewal and a stable event identity, poll with
`listModifiedRecords` and store its cursor, as the `changed-deals` example
does.

### Pickers

The connection form needs no picker, because a Zoho CRM token is bound to the
organization chosen at consent. Module, field, owner, and stage pickers for
Step configuration are not shipped. Studio setup commands use one fixed HTTPS
host each, so a picker would need one command per data center, as the Zoho
Desk organization picker has, and still could not reach a sandbox or developer
organization, whose host is the `api_domain` credential. `listModuleFields`
reads the same field and stage metadata inside a Flow.

### Other limits

- **Dex Web OAuth.** Use Dex CLI `cli-v1.4.0` or later. Zoho's token responses
  carry no `scope`, which earlier Dex Web releases reject. Dex Web sends the
  manifest scopes joined by spaces, while Zoho documents comma-separated
  scopes; whether Zoho accepts spaces is not verified against a live account.
- **Search API, deletes, conversions, notes, tasks, and related lists** have no
  operation yet.
- **COQL text with an apostrophe or backslash** cannot be matched, such as an
  account named `Macy's`, until Zoho documents an escape.

## Examples

- [`examples/lead-qualification`](examples/lead-qualification) is a runnable
  Dex Web **Start Flow** example that uses `listModuleFields`, `findRecords`,
  `upsertRecord`, `updateRecord`, and `getRecord`: it checks the requested
  stage, finds or creates the account, upserts the contact by email linked to
  the account, moves the contact's newest open deal to the stage and owner, and
  reads the deal back.
- [`examples/changed-deals`](examples/changed-deals) reads the deals changed
  since an instant or a stored cursor with `listModifiedRecords`, page by page,
  and completes with the cursor the next poll continues from.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the examples own the real
Worker, retry, persistence, duplicate-dispatch, token-refresh, and transition
coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/zoho/crm/connector.yaml
go run ./cmd/connectorctl generate --check connectors/zoho/crm/connector.yaml
```

The provider fakes cover the `Zoho-oauthtoken` header, each data center's own
Zoho Accounts server and zohoapis hosts including sandbox and developer, a
foreign `api_domain`, the one refresh and resend after a 401 and none after a
scope mismatch, non-rotating refresh tokens and terminal refresh codes, COQL
rendering and value rejection, pair-nested conditions, same-second ties and a
record changed mid-feed in the watermark cursor, `204` pages, `$` keys,
lookups, upsert actions and duplicate fields, the `DUPLICATE_DATA` resend,
record-level errors inside 2xx and 400 responses, `Retry-After`, redirects,
dropped connections, and oversized, malformed, and credential-reflecting
responses.

No live Zoho account was used. The following live behavior is unverified:
whether Zoho accepts space-separated scopes at `/oauth/v2/auth`; whether
`ZohoCRM.modules.CREATE` alone lets an upsert update an existing record;
whether the group scopes `ZohoCRM.modules.READ`, `CREATE`, and `UPDATE` are
accepted together with `ZohoCRM.coql.READ`; whether Dex Web's code exchange
maps `api_domain` as it maps Salesforce's `instance_url`; the exact COQL
grammar for nested `and`/`or`, `id >` on a bare number, and `order by
Modified_Time asc, id asc`; whether COQL text `=` ignores letter case; the
answer for a missing or deleted record on `GET` and `PUT`; the JSON shape of
`details` in `DUPLICATE_DATA` and `INVALID_DATA`; whether a single-record
write error arrives as HTTP 400 or 207; how Zoho resolves an upsert whose
duplicate-check value matches several records, and whether concurrent upserts
on a field that is not unique can both insert; whether COQL compares
`Modified_Time` in whole seconds, as the cursor assumes; the
`X-API-CREDITS-REMAINING` header; whether every 429 is returned before a write
is applied; and Zoho's default names for closed deal stages.
