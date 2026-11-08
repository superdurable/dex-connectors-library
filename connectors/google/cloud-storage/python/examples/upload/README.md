# Object upload Flow (Python)

This runnable Flow uses `uploadObject` with the static `google-cloud-storage` connection. Configure OAuth as described in the [connector guide](../../../README.md), then run `uv run python -m examples.upload.worker` from the Python package directory. The Flow stores the returned object name and generation in a declared Attribute. Upload creates an object only when its name is absent; a 412 response triggers an immutable-generation content comparison.

Run `dexcli visualize python/examples/upload/flow.py --schema-version 2.0 --lint app --json --python python/.venv/bin/python --out /tmp/google-cloud-storage-python-upload-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/upload/integration_test.py` from the Python package directory to verify the real Worker and Client path against the generated mock. Live Google authorization has not been verified.
