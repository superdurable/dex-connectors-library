# Supabase connector

This Dex connector exposes `selectRows`, `upsertRows`. The runnable [sample Flow](examples/sample-supabase/flow/workflow.go) uses `selectRows` with the static `supabase` connection. Each operation uses the generated typed Step factory.

## Provider contract

`upsertRows` uses `POST`, `Prefer: resolution=merge-duplicates,return=representation`, and `on_conflict`. The selected conflict columns must have a database-enforced primary key or unique constraint. Repeating the same rows merges the same values. Use a server-side secret API key in `apikey`; publishable keys remain subject to row-level security. Supply the project REST URL ending in `/rest/v1` as `baseUrl`; there is no shared project default.

Official documentation used for the endpoint, authentication, and response contract:

- [https://supabase.com/docs/guides/api/creating-routes](https://supabase.com/docs/guides/api/creating-routes)
- [https://supabase.com/docs/guides/getting-started/api-keys](https://supabase.com/docs/guides/getting-started/api-keys)
- [https://docs.postgrest.org/en/v14/references/api/tables_views.html](https://docs.postgrest.org/en/v14/references/api/tables_views.html)

## Authorization and configuration

Start at [https://supabase.com/dashboard/projects](https://supabase.com/dashboard/projects) and follow the authorization guide shown in Dex Web Connections. The `api_key` field is secret. It is resolved for each call and sent only in the `apikey` header. The connector never places it in a URL, Result, receipt, Failure, or log. Leave `timeout` blank for 15 seconds and `maxResponseBytes` blank for 2,097,152 bytes. A larger bounded value can be entered if a real response page exceeds that limit.

## Failure handling

| Provider outcome | Dex outcome |
| --- | --- |
| Documented success response with valid JSON shape | Happy-path branch |
| 400, 401, 403, 409 (constraint conflict), or another definite 4xx | Optional `providerRejected` branch |
| 408, 429, 5xx, or transport interruption | Retry until the Step budget expires |
| Malformed, incomplete, or oversized success response | Optional `invalidResponse` branch |
| Invalid input or credential | Optional `defect` branch |

Provider error bodies are never copied into Failures. A Flow fails when an optional branch is selected without wiring. The default retry policy is exponential from one second to five minutes, with unlimited attempts within one hour.

## Mocks

The generated `supabasemock` package provides a default response for each operation. The [real-Dex mock test](examples/sample-supabase/flow/workflow_mock_integration_test.go) constructs and runs the first operation this way:

```go
mock := supabasemock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

The mock defaults represent the same Meridian example resource across both operations.

## Verification limits

The provider HTTP behavior is tested with local `httptest` servers, and the sample's mapping has a deterministic unit test. No live provider account or Dex stack was used during this batch. Real provider authorization, permissions, response variants, and the mock-backed Dex integration remain unverified locally; CI covers the Dex integration.
