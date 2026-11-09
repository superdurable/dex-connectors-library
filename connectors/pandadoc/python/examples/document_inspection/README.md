# PandaDoc Python document inspection

This runnable Flow calls `getDocument` through the static `pandadoc` connection, persists the selected document status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/pandadoc/python`, run `uv run python -m examples.document_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `PandaDocDocumentInspectionPython` with a unique Flow ID and input `{"documentId":"fANp5wW5EwY7AkiTkTGekZ"}`. Replace the sample ID with one returned by the PandaDoc API.

Run the real Dex integration scenario with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/document_inspection/integration_test.py`.
