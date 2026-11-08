# Document discovery Flow (Python)

This runnable Flow calls `listDocuments` through the static `google-firestore` Dex Web connection. Configure OAuth in the [connector guide](../../../README.md), then run `uv run python -m examples.documents.worker` from the Python package directory. The result is stored in a declared Attribute.

Run `dexcli visualize python/examples/documents/flow.py --schema-version 2.0 --lint app --json --python python/.venv/bin/python --out /tmp/google_firestore-python-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/documents/integration_test.py` from the Python package directory to verify the real Worker and Client path against a local provider fixture. Live Firestore authorization has not been verified.
