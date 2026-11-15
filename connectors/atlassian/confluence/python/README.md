# Atlassian Confluence connector for Python

`dex-connector-atlassian-confluence==0.2.0` implements `searchPages`, `getPage`, `createPage`, `updatePage`, and `addComment` for Dex Python Flows, and from 0.2.0 the `pageChanged` poll Trigger, which starts a Flow when a page is created or updated. The package requires `dex-connectors-sdk>=0.12.0`, whose poll Trigger kit the Trigger is built on, and `dex-python-sdk>=1.28.0`.

## Connection

Use a static connection name in the Flow and Dex Web **Connections**. The [connector setup guide](../README.md#atlassian-setup) walks through creating an Atlassian OAuth 2.0 (3LO) app, enabling the Confluence API and all requested scopes, registering the exact Dex Web redirect URI, and selecting a Confluence site. Dex Web stores the OAuth client secret and rotating tokens; they must not enter Flow input, results, Attributes, Streams, or logs. The generated Python project binding resolves private or shared credentials and persists refreshed tokens through the SDK's manifest driver.

The connection fields are `cloudId`, `endpoint`, `maxResponseBytes`, and `pollInterval`. `cloudId` is a non-secret Confluence site UUID. Leave it blank only when the authorization grants exactly one Confluence site; the connector then discovers that site. `endpoint` defaults to Atlassian's public API gateway and accepts an HTTPS compatible gateway or a loopback HTTP endpoint for tests. `maxResponseBytes` is a positive byte limit for successful provider responses; the manifest supplies a safe default. `pollInterval` is the delay between `pageChanged` polls, at least 10 seconds; zero keeps the one-minute default, and operations ignore it.

## Example and verification

The [read-page example](examples/read_page/README.md) registers one Query Step and a static `confluence-pages` connection. It includes a real Dex Worker and Client integration test with an OS-assigned local provider port. The test covers a successful page read and failure of an unwired `notFound` branch. The shared conformance fixtures replay 41 Go-recorded provider scenarios; generated OAuth agreement tests replay 14 refresh cases. These are local verification paths. Live Atlassian OAuth, consent, site selection, refresh, and API behavior remain unverified.

Run the package checks from this directory:

```sh
uv sync --locked
uv run ruff check src tests examples
uv run mypy --strict src
uv run pytest tests
```

With `dexcli dev` running at a free port, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/read_page/integration_test.py`. Generate the strict FDG 2.0 with `dexcli visualize ./examples/read_page/flow.py --python .venv/bin/python --schema-version 2.0 --json --out ./build/read-page`.

## pageChanged Trigger

`pageChanged` polls Confluence CQL content search, newest modification first, at parity with the Go connector, on `dex_connectors.sdk.pollingtrigger`; the [connector README](../README.md#pagechanged-trigger) describes the query, the ten-minute lookback, the keyed snapshot of seen page versions, paging, change kinds, resets, and rate limits. The cursor token and snapshot entries match the Go connector's byte for byte, including nanosecond times, which the Go-generated fixtures in `tests/fixtures/go-page-change-cases.json` check, so a Go and a Python replica can share one binding's cursor and inbox.

- `new_project_page_trigger_runner(project, connection_name, routes)` opens the connection and each route's saved binding and returns a `PageTriggerRunner`. Every change is recorded in the binding's project Trigger inbox before its project poll cursor commits, so a restarted Worker neither misses nor repeats a change. An unsaved binding watches every space the connection can view. Await `run(context)` for the life of the Worker and `aclose()` afterwards; `wake_poller(binding_name)` polls now.
- `new_page_trigger_runner(PageTriggerRunnerConfig(...))` takes targets and cursor stores directly and returns the kit's `PollRunner`; a route without a cursor store keeps it in memory and records a new baseline on every start, as `new_page_changed_trigger` always does.
- Each `PageChangedEvent` carries the site, the derived `change_kind` (`PAGE_CHANGE_CREATED` for version 1, `PAGE_CHANGE_UPDATED` otherwise), and the page summary. The event ID hashes the site, page ID, and version, so `new_dex_flow_trigger_target` starts one Flow per page version. Python datetimes keep microseconds, so a page time with nanoseconds is truncated in the payload; the cursor keeps the exact time, and Confluence reports milliseconds.
- `define_page_changed_trigger_binding` declares a static binding from `get_connector_trigger_bindings`. Dex CLI v1.47.3 reads the binding but not a `ConnectorConfigurationUI` object passed as `configuration_ui`, so `dexcli visualize` warns `connector_configuration_ui` and Dex Web shows the binding without the space picker; the Go example's binding shows it.

The runnable [page-changed Flow](examples/page_changed/README.md) starts one Flow per page version and reads the page back as Markdown with `getPage`. Its integration tests run a real Dex Worker and Client against a local Confluence stand-in: the baseline starts no Flow, other spaces are ignored, each page version starts one Flow across a Trigger restart whose first search answers 429, and a changed space records a new baseline. Live Confluence search has not been verified.
