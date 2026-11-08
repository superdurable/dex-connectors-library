# Heap Python user-properties Flow

This runnable Flow calls `setUserProperties` through the static `heap` connection, stores acceptance in an Attribute, and completes. Optional branches are unwired and fail the Flow when selected. Enter the numeric `appId` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/heap/python`, run `uv run python -m examples.user_properties.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `HeapUserPropertiesPython` with a unique Flow ID and input `{"identity":"member-1","properties":{"Plan":"Pro"}}`. Replace the sample with one permanent unique identity and non-secret values.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
dexcli visualize examples/user_properties/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/heap-python-fdg --open=false --python .venv/bin/python
```
