# Dropbox Sign Python signature request inspection

This runnable Flow calls `getSignatureRequest` through the static `dropbox-sign` connection, persists the selected request status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/dropbox/sign/python`, run `uv run python -m examples.signature_request_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `DropboxSignSignatureRequestInspectionPython` with a unique Flow ID and input `{"signatureRequestId":"fa5c8a0b0f492d768749333ad6fcc214c111e967"}`. Replace the sample ID with one returned by the Dropbox Sign API.

Run the real Dex integration scenario with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/signature_request_inspection/integration_test.py`.
