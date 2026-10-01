# Salesforce Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Salesforce; no live Salesforce org was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The Salesforce Connector reads and writes Salesforce CRM records through the
REST API. It is the system of record in most processes, so it favors lookup
fidelity and writes that are safe to retry. It exposes operation-specific Dex
Step factories:

- `salesforce.NewQueryRecordsStep` runs one SOQL statement whose values are
  typed bind values and returns one bounded page plus a page cursor.
- `salesforce.NewGetRecordStep` reads the chosen fields of one record by
  sObject type and ID.
- `salesforce.NewUpsertRecordByExternalIDStep` creates or updates one record
  keyed by an External ID field value, which is the create a retried Step needs.
- `salesforce.NewUpdateRecordStep` updates the named fields of one record by ID.

Every request uses REST API `v62.0` unless the connection overrides
`apiVersion`, and runs with the object, field, and sharing access of the
authorized user. The connector never deletes records. This release has no
Triggers: Change Data Capture and Platform Events need a streaming
subscription design that is still open.

## Authorization

A connection selects one method:

| Method | Login host | Use it for |
| --- | --- | --- |
| `salesforce-oauth` (recommended) | `login.salesforce.com` | Production and Developer Edition orgs |
| `salesforce-sandbox-oauth` | `test.salesforce.com` | Sandboxes |
| `salesforce-jwt-bearer` | `login.salesforce.com` or `test.salesforce.com` | A pre-authorized integration user without interactive consent |

Production and sandbox are separate methods because Dex Web runs the OAuth web
server flow against fixed endpoints in the manifest, and a sandbox
authorization code can only be exchanged at the sandbox login host. Both hosts
sign in users of any org, so a My Domain URL is never entered. An org whose
administrator blocks login from `login.salesforce.com` or `test.salesforce.com`
cannot use the OAuth methods in this release.

Both OAuth methods request exactly `api` and `refresh_token`, with PKCE.
`api` allows REST calls and `refresh_token` returns the refresh token that keeps
a connection working. The guide in `connector.yaml` walks through an External
Client App: Callback URL, the two scopes, PKCE, the refresh-token policy, and
the consumer key and secret.

Salesforce's token response has no `expires_in` and carries the org's
`instance_url`, which is the base URL of every REST call. `CredentialRefreshDriver`
delegates the exchange to `sdkgo/oauthtoken` with `ClientSecretPost` at the
method's login host, `AcceptsMissingExpiresIn`, and `RetainedResponseFields`
set to `instance_url`:

- Dex Web maps `access_token`, `refresh_token`, and `instance_url` from the
  authorization response. A session without an instance URL is refreshed once
  before its first call, which fills it in.
- A session without a recorded expiry is kept until Salesforce rejects it
  (`KeepWhenExpiryMissing`). After a refresh the driver records a nominal
  two-hour expiry, Salesforce's default session timeout; a shorter org timeout
  costs one rejected call, and a longer one only refreshes early.
- A 401 `INVALID_SESSION_ID` forces one coordinated refresh and one resend. A
  second rejection selects `providerRejected`; it never loops. Salesforce
  rejects a request with 401 before applying it, so resending a write is safe.
- `invalid_grant`, `invalid_client`, `invalid_client_id`,
  `invalid_client_credentials`, `unauthorized_client`, `unsupported_grant_type`,
  `inactive_user`, `inactive_org`, and a session without the `api` or `full`
  scope require reauthorization. Every Step then selects `providerRejected`
  without calling Salesforce. A token endpoint 5xx and `rate_limit_exceeded`
  stay retryable.
- A new refresh token is stored only when the app rotates refresh tokens;
  otherwise the prior one is kept.

`salesforce-jwt-bearer` signs an RS256 assertion with the saved private key:
`iss` is the consumer key, `sub` the username, `aud` the login host, and `exp`
two minutes after `iat`. It has no `scope` claim, because Salesforce takes the
scopes from the app. The response has no refresh token, so every refresh mints
a new session with a new assertion.

The driver never owns persistence. Local development reloads and atomically
replaces the private `0600` connection file. Hosted apps receive only the
selected method, the session token, and the instance URL:
`DecodeResolvedCredentialsJSON` rejects refresh tokens, client secrets, and
private keys. `DecodeCredentialsJSON` and `EncodeCredentialsJSON` are the
broker's trusted decode and persistence hooks.

For local Dex Web setup, name the factory connection and load the same name at
application startup, as [`examples/record-sync/main.go`](examples/record-sync/main.go)
does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := salesforce.NewLocalConnection(store, recordsync.ConnectionName)
```

## Querying with bound values

`queryRecords` takes one SOQL statement with `:name` placeholders, the Apex
bind syntax Salesforce developers already know, and a map of typed values:

```go
// MapToQueryRecordsInput binds the configured names as identifiers and the match value as a string literal.
func (flow *Flow) MapToQueryRecordsInput(input Input) salesforce.QueryRecordsInput {
	return salesforce.QueryRecordsInput{SOQL: matchQuery, Bindings: map[string]salesforce.SOQLValue{
		"externalIdField": salesforce.SOQLIdentifier(flow.configuration.ExternalIDField),
		"sObjectType":     salesforce.SOQLIdentifier(flow.configuration.SObjectType),
		"matchField":      salesforce.SOQLIdentifier(flow.configuration.MatchField),
		"matchValue":      salesforce.SOQLString(input.MatchValue),
	}}
}
```

The REST API has no server-side bind parameters, so the connector renders each
value as an escaped SOQL literal. We chose bindings over a typed filter input
because SOQL is the vendor-native query language that CRM processes use as
often as simple search: a filter structure would give up relationship fields,
aggregates, `ORDER BY`, and date literals such as `LAST_N_DAYS:30`. Bindings keep
all of SOQL while making string concatenation unnecessary:

- `SOQLString`, `SOQLStringList` (for `IN`), `SOQLNumber` (a decimal written as
  text, so currency keeps its digits), `SOQLBoolean`, `SOQLNull`, `SOQLDate`,
  and `SOQLDateTime` render literals. Quotes, backslashes, and line breaks are
  escaped, so bound text never leaves its literal.
- `SOQLLikeContains` and `SOQLLikeStartsWith` build `LIKE` patterns in which
  `%` and `_` in the text match themselves.
- `SOQLIdentifier` binds a configured object or field API name, or a dotted
  relationship path, after checking that it contains only letters, digits,
  underscores, and dots.
- A placeholder inside a string literal is left alone. A placeholder without a
  binding, a binding without a placeholder, an invalid value, or a statement
  longer than one REST request selects `defect` before any request, and the
  Failure never repeats the value.

Each page asks for `batchSize` records through `Sforce-Query-Options`, 200 by
default (Salesforce's minimum) and up to 2,000. Salesforce treats it as a hint,
so `maxResponseBytes` also bounds every page; an oversized page selects
`invalidResponse`. When `isDone` is false, pass `nextRecordsCursor` back as the
next input's cursor. Salesforce expires an unused cursor after about 15
minutes, which selects `queryRejected` with `INVALID_QUERY_LOCATOR`. A cursor is
accepted only in the `nextRecordsUrl` form for the connection's API version.

`found` means the page has records, more pages remain, or an aggregate such as
`SELECT COUNT() FROM Contact` counted rows. `notFound` means the query matched
nothing, so an empty lookup is never a silent success. Records keep each field's
exact JSON value; `Record.StringField` and `Record.DecodeField` read one field.

## Reading a record

`getRecord` reads 1 to 200 fields of one record, which keeps Results small and
avoids fields the user cannot see. It selects `notFound` for `NOT_FOUND` and
`ENTITY_IS_DELETED`. Salesforce reports an unknown sObject type with the same
404, so `notFound` also covers a misspelled type. An unknown field selects
`providerRejected` with `INVALID_FIELD`. Relationship paths belong in
`queryRecords`.

## Creating without duplicates: upsert by external ID

`upsertRecordByExternalId` sends `PATCH /sobjects/{type}/{externalIdField}/{value}`.
Salesforce creates the record when no record carries the value and updates it
when exactly one does, and the Result reports `isCreated`. Repeating the same
input therefore converges on one record, so the connector retries every
ambiguous outcome instead of declaring it uncertain: a timeout, a dropped
connection, a 5xx, or an unreadable success returns Retry, and the next attempt
updates the record the first attempt created.

- Mark the field both External ID and Unique. Two attempts of one Step can run
  at once, because Dex starts another attempt when an async Execute outlasts
  its seven-second local phase; a unique field makes Salesforce reject the
  second insert with `DUPLICATE_VALUE`, which this operation retries so the
  next attempt updates the first record. Without Unique, concurrent attempts
  can create two records, and later upserts select `multipleMatches`.
- `multipleMatches` (HTTP 300) lists the record IDs that share the value and
  changes nothing.
- Salesforce runs triggers, flows, and validation again on every repeat, so a
  side effect those automations perform is repeated too.
- The request body cannot set `Id` or the external ID field, which the path
  already names. A value containing `/` is path-escaped; whether every org
  accepts it has not been verified.

`TestRecordSyncSlowUpsertSentTwiceKeepsOneRecordWithRealDex` proves the
behavior on a real Dex Server: a nine-second upsert is sent twice and the fake
provider, which serializes writes like a unique index, ends with one record.

## Updating a record

`updateRecord` sends `PATCH /sobjects/{type}/{id}` with at least one field;
fields it does not name keep their values and JSON `null` clears one. Setting
the same values again leaves the record in the same state, so a lost response,
timeout, or 5xx returns Retry, and there is no `uncertain` branch. Automations
run again on a repeat. This release has no conditional update: an
`If-Unmodified-Since` check would report a false conflict when a retry follows
an attempt that already applied, so stale-write protection needs its own
design.

## Branches and Salesforce errors

Salesforce reports errors as an array of `errorCode`, `message`, and `fields`.
The connector keeps the codes and field API names in `ProviderError` values on
the write Results, and names the first code in the Failure message. It never
keeps the message text, which can repeat record values.

| Salesforce response | Result |
| --- | --- |
| `REQUEST_LIMIT_EXCEEDED`, 429 | Retry, as a rate limit |
| `UNABLE_TO_LOCK_ROW` | Retry, as a conflict |
| `SERVER_UNAVAILABLE`, 5xx, transport failure | Retry |
| `DUPLICATE_VALUE` on upsert | Retry, as a concurrent attempt |
| 401, `INVALID_SESSION_ID` after the one refresh | `providerRejected` (authentication) |
| `INSUFFICIENT_ACCESS`, `INSUFFICIENT_ACCESS_OR_READONLY`, `API_DISABLED_FOR_ORG`, 403 | `providerRejected` (authorization) |
| `NOT_FOUND`, `ENTITY_IS_DELETED`, 404 | `notFound` on `getRecord` and `updateRecord`; `providerRejected` on `queryRecords` and upsert, where it means an unknown path, object, or external ID field |
| Other 400, 409, 412, 422, such as `MALFORMED_QUERY`, `INVALID_FIELD`, `REQUIRED_FIELD_MISSING`, `FIELD_CUSTOM_VALIDATION_EXCEPTION`, `DUPLICATES_DETECTED`, `ENTITY_IS_LOCKED` | `queryRejected`, `recordRejected`, or `providerRejected` on `getRecord` |

`REQUEST_LIMIT_EXCEEDED` covers both the concurrent long-request limit, which
clears in seconds, and the org's 24-hour API allocation, which does not; the
second exhausts the two-minute retry window and fails the Step. Applications
that expect to reach the daily allocation can widen the window with
`StepOptionsOverride` or add `ProceedToOnExecuteFailure`. Invalid input and
credentials that cannot be used select `defect` before any request. Only each
operation's happy-path branch is required; every other branch is optional and
fails the Flow when unwired.

## Studio units

The manifest declares two configuration units and no Studio command:

- `sObjectPicker` stores an object API name. It lists standard CRM objects and
  accepts a custom object's API name from Setup > Object Manager.
- `fieldNameInput` stores one validated field API name, such as an External ID
  field.

Studio commands only reach fixed HTTPS hosts, while every org answers on its
own instance URL, so the units cannot list an org's objects or fields with
describeGlobal. Listing needs a Studio command whose host comes from the
connection's `instance_url`. The `ui/` package builds the credential-safe
bundle published as `connector-ui.tgz`, rendering the connection status and
these units from the shared `@superdurable/dex-connectors-react` components.

## Example

[`examples/record-sync`](examples/record-sync) is a runnable Dex Web
**Start Flow** example that matches a record by a field value, links the one
match to an external ID or creates the record by that ID, and reads it back.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

With the pinned Dex development server running, the same module owns its real
Worker, retry, persistence, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```
