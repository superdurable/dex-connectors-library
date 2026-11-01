# Supabase connector for Python

Install with `pip install dex-connector-supabase==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest operations `selectRows` (Query) and `upsertRows` (Mutation) with the
same branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `selectRows` | `new_select_rows_step` | `selected` | `providerRejected`, `invalidResponse`, `defect` |
| `upsertRows` | `new_upsert_rows_step` | `upserted` | `providerRejected`, `invalidResponse`, `defect` |

`selectRows` reads one PostgREST page of an exposed table: `columns` is a
comma-separated identifier list (blank selects all), `limit` ranges from 1
through 1000 (zero means 100), and the JSON row array is returned untouched.
`upsertRows` posts 1 through 100 rows with
`Prefer: resolution=merge-duplicates,return=representation` and
`on_conflict=<onConflict>`. Every row must carry non-null, distinct values for
the conflict columns, which the database must declare unique, so a retry merges
into the same rows. HTTP 201 or 200 with a row array selects `upserted`.

Both operations retry on 408, 429, 5xx, and transport failures; other non-200
statuses select `providerRejected` with authentication (401), authorization
(403), or validation (400, 422) kinds.

## Authorization

Create a Dex Web connection named `supabase` as described in the
[connector README](../README.md#authorization-and-configuration), with the
project's Data API URL as `baseUrl`. The API key travels only in the `apikey`
header and is resolved for every attempt.

## Example

[`examples/sample_supabase`](examples/sample_supabase/flow.py) selects one
page of rows. Run the Worker with
`uv run python -m examples.sample_supabase.worker` after `dexcli dev` has
loaded a project containing the `supabase` connection.

## Tests

`supabasemock.Mock` scripts both operations and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
