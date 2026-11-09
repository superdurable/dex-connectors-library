# OneSpan Sign Python package inspection

This runnable Flow calls `getPackage` through the static `onespan-sign` connection, persists the selected package status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/onespan/sign/python`, run `uv run python -m examples.package_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `OneSpanSignPackageInspectionPython` with a unique Flow ID and input `{"packageId":"9sKhW-h-qS9m6Ho3zRv3n2a-rkI="}`. Replace the sample ID with one returned by OneSpan Sign.
