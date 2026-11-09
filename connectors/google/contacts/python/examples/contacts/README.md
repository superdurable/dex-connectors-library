# Google Contacts list Flow (Python)

This runnable Flow uses `listContacts` with the static `google-contacts` connection. Run `uv run pytest tests/test_manifest_contract.py` to check registration. From the connector directory, generate FDG 2.0 with `dexcli visualize python/examples/contacts/flow.py --schema-version 2.0 --lint app --json --python python/.venv/bin/python --out /tmp/google-contacts-python-fdg --open=false`.

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/contacts/integration_test.py` for the real Worker and Client path. Configure the Google OAuth connection in Dex Web before running `uv run python -m examples.contacts.worker` against Google. Live Google authorization has not been verified here.
