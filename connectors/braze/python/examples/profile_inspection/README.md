# Braze Python profile inspection

This runnable Flow calls `getUserProfile` through the static `braze` connection, persists selected profile fields, and completes. Optional branches are unwired and fail the Flow when selected. Configure `rest_api_key` and the account's `baseUrl` in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/braze/python`, run `uv run python -m examples.profile_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `BrazeProfileInspectionPython` with a unique Flow ID and input `{"externalId":"sample-user-1"}`. Replace the sample ID with a real external user ID.

For strict FDG 2.0 verification, run:

```bash
dexcli visualize examples/profile_inspection/flow.py --language python --python .venv/bin/python --schema-version 2.0 --lint app --json --out /tmp/braze-python-fdg --open=false
```

The integration test exercises a real Dex Worker and Client against a local provider fake and retries a 503 before persisting the response.
