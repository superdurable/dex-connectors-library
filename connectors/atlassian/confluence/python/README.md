# Atlassian Confluence connector for Python

`dex-connector-atlassian-confluence==0.1.0` implements `searchPages`, `getPage`, `createPage`, `updatePage`, and `addComment` for Dex Python Flows. It has no Python Trigger. The package requires `dex-connectors-sdk==0.6.0` and `dex-python-sdk>=1.28.0`.

## Connection

Use a static connection name in the Flow and Dex Web **Connections**. The [connector setup guide](../README.md#atlassian-setup) walks through creating an Atlassian OAuth 2.0 (3LO) app, enabling the Confluence API and all requested scopes, registering the exact Dex Web redirect URI, and selecting a Confluence site. Dex Web stores the OAuth client secret and rotating tokens; they must not enter Flow input, results, Attributes, Streams, or logs. The generated Python project binding resolves private or shared credentials and persists refreshed tokens through the SDK 0.6.0 manifest driver.

The connection fields are `cloudId`, `endpoint`, and `maxResponseBytes`. `cloudId` is a non-secret Confluence site UUID. Leave it blank only when the authorization grants exactly one Confluence site; the connector then discovers that site. `endpoint` defaults to Atlassian's public API gateway and accepts an HTTPS compatible gateway or a loopback HTTP endpoint for tests. `maxResponseBytes` is a positive byte limit for successful provider responses; the manifest supplies a safe default.

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
