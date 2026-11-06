# Google Docs policy read

This Flow reads the first tab of a Google Doc as plain text. It uses the static
`google-docs-policy` connection and fails if an optional provider branch is selected.

From `connectors/google/docs/python`, install with `uv sync --locked` and generate
strict FDG 2.0 with:

```sh
mkdir -p build
dexcli visualize ./examples/policy_read/flow.py --language python \
  --python .venv/bin/python --schema-version 2.0 --json \
  --out ./build/google-docs-policy-read
```

Start `dexcli dev -flow-rendering-dir ./build`, configure the Google OAuth
`google-docs-policy` connection in Dex Web following the parent README, then
export the owner-only `DEX_CONNECTOR_CONFIG_FILE` path and
`DEX_FLOW_SERVICE_ADDRESS` from that project. Run the Worker and start a
Flow with a unique ID and a document ID from the
`/document/d/{documentId}/edit` portion of its Google Docs URL:

```sh
uv run python -m examples.policy_read.worker
uv run python -m examples.policy_read.start policy-read-001 YOUR_DOCUMENT_ID
```

The Dex integration test uses a local provider fake and needs no Google
authorization: `uv run pytest examples/policy_read/integration_test.py`
while `dexcli dev` is running.
