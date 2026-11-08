# Google Contacts creation Flow (Python)

This runnable Flow uses `createContact` with the static `google-contacts` connection. Start it with a nonempty `givenName` and optional `familyName` and `emailAddress`. It persists the confirmed `people/{id}` contact resource name.

From the connector directory run `dexcli visualize python/examples/creation/flow.py --schema-version 2.0 --lint app --json --python python/.venv/bin/python --out /tmp/google-contacts-creation-python-fdg --open=false`. From `python/`, run `uv run pytest tests/test_manifest_contract.py` and, with `dexcli dev` running, `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/creation/integration_test.py` to exercise the Flow through real Dex with its generated mock. Configure Google OAuth in Dex Web before running `uv run python -m examples.creation.worker` against Google. Live Google authorization has not been verified here.
