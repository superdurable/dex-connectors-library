# Notion Python connector

`dex-connector-notion` provides Dex-native `search`, `queryDatabase`, `getPage`,
`createPage`, and `updatePageProperties` operations and, from 0.2.0, the
`databaseRowChanged` poll Trigger. It requires a Notion internal connection API
token and sends the `Notion-Version: 2026-03-11` header.

Install with `pip install dex-connector-notion==0.2.1`. Use the generated,
operation-specific Step factories exported from `dex_connectors.notion`.
The checked-in [submission intake Flow](examples/submission_intake/flow.py)
shows all five factories, typed inputs, a static connection name, and required
happy-path wiring.

The [connector README](../README.md) explains Notion authorization, data
source IDs, typed filters, branches, and duplicate-write semantics. In
particular, `createPage` uses a Dex heartbeat fence before each POST, and from
0.2.1 resends an unknown outcome only while possible sends stay below the
Step's send cap, selecting `uncertain` at the cap; Notion does not accept an
idempotency key. `updatePageProperties` sends
absolute values and can retry safely. Every non-happy-path branch is optional
and fails the Flow if an application leaves it unwired.

Number, formula, and rollup values use positional decimal `plain_text`, including
small fractions. For URL, email, and phone properties, only an empty input clears
the value; surrounding whitespace is trimmed before validation. Date and filter
date-times require RFC 3339 seconds and a `Z` or colon-separated offset.

## databaseRowChanged Trigger

`databaseRowChanged` delivers each row created or edited in one data source,
at parity with the Go Trigger and on the Python Connector SDK's durable poll
Trigger kit (`dex-connectors-sdk` 0.12.0 or newer). It polls
`POST /v1/data_sources/{id}/query` with a `last_edited_time` `on_or_after`
filter, sorted ascending, and needs only the **Read content** capability.

- **Baseline.** The first poll reads the newest `last_edited_time` with one
  descending one-row query, then remembers every row edited in the two minutes
  before it, so no existing row is delivered. An empty data source starts at
  the Unix epoch, not the host clock.
- **Cursor.** The cursor holds the newest `last_edited_time` read, 96-bit
  digests of the rows seen inside the two-minute lookback, and Notion's
  `start_cursor` while a query spans pages. Each poll queries from the high
  water minus the lookback, so shared, rounded, or late-indexed timestamps
  are delivered once. At most 2,000 seen rows and 48 KiB of them are kept; an
  evicted row can repeat with the same event ID, which a Dex Flow target does
  not start twice.
- **Events.** `DatabaseRowChangedEvent` carries the data source ID, the row as
  `Page`, and `change_kind` (`DatabaseRowChangeKind`): `created` when
  `created_time` equals `last_edited_time`, else `updated`. The event ID
  hashes the data source, row ID, and `last_edited_time`. Trashed and archived
  rows are not returned by the query, so they are not delivered.
- **Limits.** Each request reads `pollPageSize` rows and a poll follows at most
  ten pages; a sequence ends at Notion's 10,000-row query limit.
- **Failures.** 429 `rate_limited` and 529 `service_overload` pause every
  binding of the connection for `Retry-After`. A 400 `validation_error` for a
  stored `start_cursor` restarts the query without a gap; a cursor for another
  data source or an undecodable one records a new baseline. A 404, a revoked
  token, or `public_api_request_blocked` backs off per binding.

- `new_project_database_row_trigger_runner(project, connection, config)`
  records each change in the binding's project Trigger inbox before the cursor
  commits in its project poll cursor, and requires a saved data source.
- `new_database_row_trigger_runner` takes targets and cursor stores directly;
  `new_database_row_changed_trigger` keeps the cursor in memory, so each start
  records a new baseline.
- `define_database_row_changed_trigger_binding` declares the binding in a
  Flow's `get_connector_trigger_bindings`, and
  `decode_database_row_changed_event_payload` decodes inbox payloads written by
  Go or Python.
- The client's `poll_interval_seconds` must be at least 5 and
  `poll_page_size` 1 to 100; zero keeps the manifest defaults.

The fetcher sends the same queries, commits the same cursor documents byte for
byte, and derives the same Trigger event IDs as Go, so Go and Python replicas
can share one binding's cursor and inbox. `tests/test_database_row_go_fixtures.py`
replays polls recorded from the Go fetcher. The
[row-changed example](examples/row_changed/README.md) starts one
`NotionDatabaseRowChangePython` Flow per row version and has real Dex
integration tests. Dex CLI v1.47.3 does not yet show the Python binding's
`dataSourcePicker` unit in Dex Web; the example README says how to save the
binding configuration.

Run `uv sync --group dev` and `uv run pytest -q tests` in this directory to
replay the Go-recorded provider scenarios and check input encoding. Run the
[real Dex integration test](examples/submission_intake/README.md) with a
local `dexcli dev` server. The provider scenarios and example use inert
credentials and mocks. Live Notion authorization, API calls, data source query
polling, and Dex Web Connections setup remain unverified.
