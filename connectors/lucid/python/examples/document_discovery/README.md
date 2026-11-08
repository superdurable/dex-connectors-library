# Lucid Python document discovery

This runnable Flow calls `listDocuments` through the static `lucid` connection, persists the selected page, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/lucid/python`, run `uv run python -m examples.document_discovery.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `LucidDocumentDiscoveryPython` with a unique Flow ID and input `{"pageToken":""}`. Copy the returned `nextPageToken` into `pageToken` to read another page.

Run `uv run pytest -q` for deterministic package tests. With `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex service, run `uv run pytest examples/document_discovery/integration_test.py -q` for the real Worker and Client path.
