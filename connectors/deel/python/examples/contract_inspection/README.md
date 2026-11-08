# Deel Python contract inspection

This runnable Flow calls `getContract` through the static `deel` connection, persists the selected contract status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/deel/python`, run `uv run python -m examples.contract_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `DeelContractInspectionPython` with a unique Flow ID and input `{"contractId":"123e4567-e89b-12d3-a456-426614174000"}`. Replace the sample contract ID with one returned by Deel.
