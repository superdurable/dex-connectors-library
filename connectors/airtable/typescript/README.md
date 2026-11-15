# Airtable TypeScript connector

`@superdurable/connector-airtable` supplies `listRecords`, `getRecord`,
`upsertRecords`, and `updateRecords` for Dex TypeScript Flows, and the
`recordChanged` poll Trigger at parity with the Go module. The operation
specific Step factories, model validators, connection contracts, and mocks are
generated from [`connector.yaml`](../connector.yaml) and the Go model types.
After changing either source, run `go run ./cmd/connectorctl generate --lang
typescript connectors/airtable/connector.yaml` from the repository root.

## Install and connect

```sh
npm install @superdurable/connector-airtable @superdurable/connectors-sdk @superdurable/dex
```

Use Node.js 22 or 24, Connectors SDK 0.12.0, and Dex TypeScript SDK 1.45.4 or
later. Create a personal
access token at [Airtable > Developer Hub > Personal access tokens](https://airtable.com/create/tokens).
Grant `data.records:read` for the read operations and `data.records:write` for
the mutations, and select each base the Flow needs. Add `schema.bases:read` if
the Dex Web base, table, and field pickers or the `recordChanged` Trigger are
used. Store the token as the private
`personal_access_token` credential in a Dex Web Connection. Its value must
never enter Flow input, Attributes, results, Streams, or logs.

At Worker startup, load the project with `loadFromEnvironment()` from
`@superdurable/connectors-sdk/projectconfig` and pass the static
`ConnectionName` to `newProjectConnection`. The [runnable example](examples/list-records/)
contains that code. The connection resolves the current private token before
every request; `endpoint`, `maxResponseBytes`, `pollInterval`, and
`pollPageSize` use manifest defaults when blank.

## Operations

| Factory | Request | Happy branch |
| --- | --- | --- |
| `newListRecordsStep` | `POST /v0/{baseId}/{table}/listRecords` | `listed` |
| `newGetRecordStep` | `GET /v0/{baseId}/{table}/{recordId}` | `found` |
| `newUpsertRecordsStep` | `PATCH /v0/{baseId}/{table}` with `performUpsert` | `upserted` |
| `newUpdateRecordsStep` | `PATCH /v0/{baseId}/{table}` with record IDs | `updated` |

All other declared branches are optional. An unwired optional branch fails
the Flow. The two mutations derive a stable key from the Dex Call ID, but
Airtable has no idempotency header: merge fields make an upsert repeat safe,
and absolute field values make an update repeat safe. The upsert Step uses
sync durability because concurrent duplicate upserts could create two rows.

`listRecords` sends filters, fields, sort, view, page size, and offset in a
JSON body. Typed filters become an Airtable formula; a raw `formula` should
come from application code, not end users. `RecordPage.offset` carries the
next page cursor. Field values use Airtable's JSON cell types. A missing
field means Airtable omitted an empty cell, while explicit JSON `null` clears
one on write.

Failure and not-found branch results contain empty `records` and record ID
lists, and an empty `fields` map for a missing record.

## recordChanged Trigger

`recordChanged` starts a Flow for each record created or changed in one picked
table, as reported by a picked Last modified time field with a time of day. It
runs on the SDK's durable poll Trigger kit
(`@superdurable/connectors-sdk/pollingtrigger`) with the Go connector's
requests, cursor, and limits: one schema read to find the field's current
name, then `POST /v0/{baseId}/{tableId}/listRecords` filtered to changes after
the cursor's time minus six seconds, oldest first, in pages of the
connection's `pollPageSize`. The [connector README](../README.md#recordchanged)
describes the seed without backlog, the five-second overlap, the 2,000-record
bound, resumed scans, expired offsets, rate limits, and what the Trigger
cannot report.

| API | Use |
| --- | --- |
| `defineRecordChangedTriggerBinding` | Declares the binding in a Flow's `getConnectorTriggerBindings`. |
| `newProjectRecordChangedTriggerRunner` | Durable runner for bindings saved in the project: each change is recorded in the binding's project Trigger inbox before its project poll cursor commits. |
| `newRecordChangedTriggerRunner` | Runner over explicit routes with cursors and targets, and an optional `requestLimiter`. |
| `newRecordChangedTrigger` | One binding's `TriggerRunner` with its cursor in memory, for tests and demonstrations. |
| `decodeRecordChangedEventPayload` | Decodes a replayed inbox payload written by Go, Python, or TypeScript. |

`RecordChangedEvent` carries `baseId`, `tableId`, `lastModifiedTime` (Go's
RFC 3339 UTC text), `isCreatedRecord`, and the `record` with fields keyed by
name. The event ID is the record ID and its modification time, such as
`recXXXXXXXXXXXXXX-20261115T090000123000000Z`. Times are kept as nanosecond
`bigint`s, so cursor tokens and event IDs match Go's byte for byte and Go and
TypeScript replicas can share a binding's cursor and inbox. `pollIntervalMs`
must be at least 1000 and `pollPageSize` from 1 to 100. A 429, or a per-base
request queue over two seconds, pauses every binding of the connection for at
least 30 seconds; other failed polls back off up to five minutes and keep the
cursor. The `logger` client option receives the runner's records, which never
carry field values, field names, or the token.

The [record-changed intake example](examples/record-changed-intake/) starts
one Flow per change, which reads its record back with the `getRecord` Step;
the Trigger and the Step share the static `airtable-intake` connection.

## Verify

```sh
npm ci
npm run check
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, run
`npm run test:integration`. The tests run the checked-in Flows through a real
Dex Worker and Client against local Airtable fixtures: the list example,
including an unwired authorization failure, and the record-changed example,
where existing records start no Flow, each create and edit starts one, a
renamed field keeps working, a change made while the runner is stopped
arrives after a restart that first answers 503, and the example runner polls
the binding saved in the project. `test/record_changed_trigger.test.ts` runs
the Go fetcher scenarios and compares every cursor token and event ID with
digests the Go fetcher produced for the same scenarios. The 33 shared provider scenarios compare
requests, outputs, branches, retries, and stable mutation keys with Go.
No live Airtable token, base, picker, or provider call was verified.
