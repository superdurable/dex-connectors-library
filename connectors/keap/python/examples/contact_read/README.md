# Keap Python contact lookup

This runnable Flow reads one Keap contact with `getContact` through the static `keap` connection and persists its profile. Configure secret `personalAccessToken` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

From `connectors/keap/python`, run `uv run python -m examples.contact_read.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `KeapContactReadPython` with a unique Flow ID and input `{"contactId":"123"}`. Replace the sample ID with the ID of a contact in your Keap CRM.
