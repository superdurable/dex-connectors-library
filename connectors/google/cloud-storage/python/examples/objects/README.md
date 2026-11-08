# Object list Flow (Python)

This runnable Flow uses `listObjects` with the static `google-cloud-storage` connection. Configure OAuth as described in the [connector guide](../../../README.md), then run `uv run python -m examples.objects.worker` from the Python package directory. The Flow stores the returned page in a declared Attribute.

Run `dexcli visualize python/examples/objects/flow.py --schema-version 2.0 --lint app --json --python python/.venv/bin/python --out /tmp/google-cloud-storage-python-objects-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/objects/integration_test.py` from the Python package directory to verify real Worker, Client, and retry behavior against a local provider fixture. Live Google authorization has not been verified.
