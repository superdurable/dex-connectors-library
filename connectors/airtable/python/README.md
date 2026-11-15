# Airtable connector for Python

Install with `pip install dex-connector-airtable==0.2.0`. The package requires
`dex-connectors-sdk>=0.12.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `listRecords` and `getRecord` and Mutations `upsertRecords` and
`updateRecords` with the same branches, defaults, and failure classification as
the Go module, and the `recordChanged` poll Trigger at parity with Go.

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

## recordChanged Trigger

`recordChanged` starts a Flow for each record created or changed in one picked
table, as reported by a picked Last modified time field with a time of day. It
runs on the SDK's durable poll Trigger kit (`dex_connectors.sdk.pollingtrigger`)
with the Go connector's requests, cursor, and limits: one schema read to find
the field's current name, then `POST /v0/{baseId}/{tableId}/listRecords`
filtered to changes after the cursor's time minus six seconds, oldest first, in
pages of the connection's `pollPageSize`. The
[connector README](../README.md#recordchanged) describes the seed without
backlog, the five-second overlap, the 2,000-record bound, resumed scans,
expired offsets, rate limits, and what the Trigger cannot report.

| API | Use |
| --- | --- |
| `define_record_changed_trigger_binding` | Declares the binding in a Flow's `get_connector_trigger_bindings`. |
| `new_project_record_changed_trigger_runner` | Durable runner for bindings saved in the project: each change is recorded in the binding's project Trigger inbox before its project poll cursor commits. |
| `new_record_changed_trigger_runner` | Runner over explicit `RecordChangedTriggerRoute` cursors and targets, with an optional `request_limiter`. |
| `new_record_changed_trigger` | One binding's `TriggerRunner` with its cursor in memory, for tests and demonstrations. |
| `decode_record_changed_event_payload` | Decodes a replayed inbox payload written by Go or Python. |

`RecordChangedEvent` carries `base_id`, `table_id`, `last_modified_time`,
`is_created_record`, and the `record` with fields keyed by name. The event ID is
the record ID and its modification time, such as
`recXXXXXXXXXXXXXX-20261115T090000123000000Z`. Cursor tokens and event IDs
match Go's byte for byte, nanoseconds included, so Go and Python replicas can
share a binding's cursor and inbox. `Config.poll_interval_seconds` must be at
least one second and `Config.poll_page_size` from 1 to 100; zero selects the
manifest default. A 429, or a per-base request queue over two seconds, pauses
every binding of the connection for at least 30 seconds; other failed polls
back off up to five minutes and keep the cursor.

Dex CLI v1.47.3 reads the Python binding but reports
`connector_configuration_ui` for its `ConnectorConfigurationUI` literal, so Dex
Web may not show the base, table, and field pickers for the Python binding. The
binding configuration is then saved as JSON with `baseId`, `tableId`, and
`fieldId`, as the example README shows.

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

[`examples/record_changed_intake`](examples/record_changed_intake/README.md)
starts one Flow per created or changed record through the `recordChanged`
Trigger, and each Flow reads its record back with the `getRecord` Step; a
Worker entrypoint polls the binding saved in the project. Its static
connection name is `airtable-intake`, for both the Trigger and the Step.

## Tests

`airtablemock.Mock` scripts every operation and defaults to the manifest
answers, including the three-page policy listing. Run `uv run pytest tests`
for unit and Go-recorded conformance checks.
`tests/test_record_changed_trigger.py` runs the Go fetcher scenarios against a
local table stand-in and compares every cursor token and event ID with digests
the Go fetcher produced for the same scenarios. The example integration tests
run against a real local Dex Worker and Client with provider fakes. Live
Airtable authorization and provider behavior have not been verified.
