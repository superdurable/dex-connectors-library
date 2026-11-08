# Jotform Python form-inspection Flow

This runnable Flow lists one page of form IDs, titles, and statuses. It stores the page in an Attribute and completes with the typed result. Optional provider branches are unwired and fail the Flow.

Configure the `jotform / jotform` connection in Dex Web using the [connector guide](../../../README.md). From this package, run `uv run python -m examples.form_inspection.worker`. Start `JotformFormInspection` with a unique Flow ID and `{"offset":1,"limit":2}`.

For a mock-provider test across a real Dex Worker, Client, and retry boundary, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 uv run pytest examples/form_inspection/integration_test.py`. Live provider authorization remains unverified.
