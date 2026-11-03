# Notion Python connector

`dex-connector-notion` provides Dex-native `search`, `queryDatabase`, `getPage`,
`createPage`, and `updatePageProperties` operations. It requires a Notion
internal connection API token and sends the `Notion-Version: 2026-03-11` header.

Install with `pip install dex-connector-notion==0.1.0`. Use the generated,
operation-specific Step factories exported from `dex_connectors.notion`.
The checked-in [submission intake Flow](examples/submission_intake/flow.py)
shows all five factories, typed inputs, a static connection name, and required
happy-path wiring.

The [connector README](../README.md) explains Notion authorization, data
source IDs, typed filters, branches, and duplicate-write semantics. In
particular, `createPage` uses a Dex heartbeat fence before its one POST;
Notion does not accept an idempotency key. `updatePageProperties` sends
absolute values and can retry safely. Every non-happy-path branch is optional
and fails the Flow if an application leaves it unwired.

Number, formula, and rollup values use positional decimal `plain_text`, including
small fractions. For URL, email, and phone properties, only an empty input clears
the value; surrounding whitespace is trimmed before validation. Date and filter
date-times require RFC 3339 seconds and a `Z` or colon-separated offset.

Run `uv sync --group dev` and `uv run pytest -q tests` in this directory to
replay the Go-recorded provider scenarios and check input encoding. Run the
[real Dex integration test](examples/submission_intake/README.md) with a
local `dexcli dev` server. The provider scenarios and example use inert
credentials and mocks. Live Notion authorization, API calls, and Dex Web
Connections setup remain unverified.
