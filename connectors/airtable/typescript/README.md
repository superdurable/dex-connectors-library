# Airtable TypeScript connector

`@superdurable/connector-airtable` supplies `listRecords`, `getRecord`,
`upsertRecords`, and `updateRecords` for Dex TypeScript Flows. The operation
specific Step factories, model validators, connection contracts, and mocks are
generated from [`connector.yaml`](../connector.yaml) and the Go model types.
After changing either source, run `go run ./cmd/connectorctl generate --lang
typescript connectors/airtable/connector.yaml` from the repository root.

## Install and connect

```sh
npm install @superdurable/connector-airtable @superdurable/connectors-sdk @superdurable/dex
```

Use Node.js 22 or 24 and Dex TypeScript SDK 1.28.0 or later. Create a personal
access token at [Airtable > Developer Hub > Personal access tokens](https://airtable.com/create/tokens).
Grant `data.records:read` for the read operations and `data.records:write` for
the mutations, and select each base the Flow needs. Add `schema.bases:read` if
the Dex Web base and table pickers are used. Store the token as the private
`personal_access_token` credential in a Dex Web Connection. Its value must
never enter Flow input, Attributes, results, Streams, or logs.

At Worker startup, load the project with `loadFromEnvironment()` from
`@superdurable/connectors-sdk/projectconfig` and pass the static
`ConnectionName` to `newProjectConnection`. The [runnable example](examples/list-records/)
contains that code. The connection resolves the current private token before
every request; `endpoint` and `maxResponseBytes` use manifest defaults when
blank.

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

## Verify

```sh
npm ci
npm run check
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, run
`npm run test:integration`. The test runs the checked-in Flow through a real
Dex Worker and Client against a local Airtable HTTP fixture, including an
unwired authorization failure. The 33 shared provider scenarios compare
requests, outputs, branches, retries, and stable mutation keys with Go.
No live Airtable token, base, picker, or provider call was verified.
