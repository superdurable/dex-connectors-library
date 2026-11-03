# Supabase connector for TypeScript

Install `@superdurable/connector-supabase@0.1.0`. This package provides the
`selectRows` Query and `upsertRows` Mutation for Dex TypeScript applications.
It exports generated models, operation definitions, named connection helpers,
Step factories, and manifest mocks.

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `selectRows` | `newSelectRowsStep` | `selected` | `providerRejected`, `invalidResponse`, `defect` |
| `upsertRows` | `newUpsertRowsStep` | `upserted` | `providerRejected`, `invalidResponse`, `defect` |

`selectRows` reads one bounded PostgREST page. Its `columns` field is a comma
separated identifier list; blank selects all columns. Its `limit` ranges from
1 through 1000, with zero selecting 100. `upsertRows` merges 1 through 100
complete rows on the database enforced unique `onConflict` columns. It sends
`Prefer: resolution=merge-duplicates,return=representation` and returns the
provider's JSON row array. Repeated writes use the same conflict keys, so
transport, 408, 429, and 5xx failures retry safely. Invalid local input and
credentials choose `defect`; malformed or oversized success responses choose
`invalidResponse`. Failure messages never include provider error bodies.

Configure the Dex Web connection named `supabase` with the project Data API URL
ending in `/rest/v1` and the private `api_key` field. See the [connector
configuration guide](../README.md#authorization-and-configuration). The
`timeout` field is a positive Go duration and `maxResponseBytes` is a positive
byte count; blank uses the manifest defaults. The client resolves the API key
for each request and sends it only in the `apikey` header.

The runnable [select rows example](examples/select-rows/README.md) uses the
static `supabase` connection. The generated mock cases are available through
`@superdurable/connector-supabase/mock`.

Run `npm ci`, `npm run check`, and, with a local Dex server address in
`DEX_FLOW_SERVICE_ADDRESS`, `npm run test:integration`. The conformance suite
replays Go recorded provider requests and outcomes. Live Supabase authorization
and provider behavior have not been verified.
