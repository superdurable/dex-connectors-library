# Airtable connector for Python

Install with `pip install dex-connector-airtable==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `listRecords` and `getRecord` and Mutations `upsertRecords` and
`updateRecords` with the same branches, defaults, and failure classification as
the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listRecords` | `new_list_records_step` | `listed` | `offsetExpired`, `providerRejected`, `invalidResponse`, `defect` |
| `getRecord` | `new_get_record_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `upsertRecords` | `new_upsert_records_step` | `upserted` | `providerRejected`, `invalidResponse`, `defect` |
| `updateRecords` | `new_update_records_step` | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`listRecords` uses Airtable's POST listRecords form. Typed `FieldEqualityFilter`
values are escaped and ANDed with an optional raw formula (see
`build_filter_formula`); the formula sent is returned in `filter_formula`.
`upsertRecords` writes 1 to 10 records matched by 1 to 3 merge fields, so a
retried write updates the records the first attempt created; it uses sync
durability so two attempts never overlap. `updateRecords` sets absolute values
by record ID. Cell values keep Airtable's JSON form, and `None` clears a field.

Requests to one base are spaced 200 ms apart within one process; a queue longer
than two seconds, or a 429, which holds the base for 30 seconds, retries. 408
and 5xx other than 501 retry; an expired list offset selects `offsetExpired`;
401, 403, 400, 413, 422, and redirects select `providerRejected` with the Go
failure kinds and Airtable's error type, never its message text.

The Python example passes its base and table as Flow input. The manifest's
`basePicker` and `tablePicker` units remain available in Dex Web Connections
for applications that use the generated setup UI.

## Authorization

Create a Dex Web connection as described in the
[connector README](../README.md#authentication). The personal access token
travels only in the `Authorization: Bearer` header and is resolved for every
attempt.

## Example

[`examples/policy_lookup`](examples/policy_lookup/README.md) is the policy lookup
Step of the Go refund-decision example, with the base and table passed as start
input. It includes Worker and Client entrypoints and strict FDG 2.0 generation
instructions. Its static connection name is `airtable-refund-policies`.

## Tests

`airtablemock.Mock` scripts every operation and defaults to the manifest
answers, including the three-page policy listing. Run `uv run pytest tests`
for unit and Go-recorded conformance checks. The example integration test runs
against a real local Dex Worker and Client with an inert provider fake. Live
Airtable authorization and provider behavior have not been verified.
