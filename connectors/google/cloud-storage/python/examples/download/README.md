# Object download Flow (Python)

This runnable Flow uses `downloadObject` with the static `google-cloud-storage` connection. Configure OAuth as described in the [connector guide](../../../README.md), then run `uv run python -m examples.download.worker` from the Python package directory. The Flow stores Base64 content and its media type in a declared Attribute. Download only content the application intends to persist in Dex.

Run `dexcli visualize python/examples/download/flow.py --schema-version 2.0 --lint app --json --python python/.venv/bin/python --out /tmp/google-cloud-storage-python-download-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/download/integration_test.py` from the Python package directory to verify the real Worker and Client path against a local provider fixture. Live Google authorization has not been verified.
