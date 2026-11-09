# Lever Python opportunity inspection

This runnable Flow calls `getOpportunity` through the static `lever` connection, persists the selected opportunity stage, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/lever/python`, run `uv run python -m examples.opportunity_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `LeverOpportunityInspectionPython` with a unique Flow ID and input `{"opportunityId":"250d8f03-738a-4bba-a671-8a3d73477145"}`. Replace the sample opportunity ID with one returned by Lever.
