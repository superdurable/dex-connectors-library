# monday.com work request

This Flow lists one board page, creates an item, sets its status column to `Done`, adds a plain-text update, and reads the item back. It uses the static connection name `monday-work-requests`. Optional provider branches remain unwired and fail the Flow for review.

Generate a strict Flow Definition Graph 2.0 from `connectors/monday/python`:

```bash
mkdir -p build
uv run dexcli visualize ./examples/work_request/flow.py --schema-version 2.0 --json --out ./build/work_request
```

Configure the `monday-work-requests` connection in Dex Web using a personal token or the OAuth setup guide. From `connectors/monday/python`, run `uv sync --locked`, start `dexcli dev -dex-port 45140 -web-port 45141 -open=false`, then run the Worker with `DEX_FLOW_SERVICE_ADDRESS=localhost:45140 uv run python -m examples.work_request.worker`. In another terminal, run `DEX_FLOW_SERVICE_ADDRESS=localhost:45140 uv run python -m examples.work_request.start <unique-flow-id> examples/work_request/input.example.json` after changing the board ID to one in your account.

The checked-in `integration/test_work_request.py` uses a fake monday.com API with a real Dex Worker and Client; run it with `DEX_FLOW_SERVICE_ADDRESS=localhost:45140 uv run pytest integration/test_work_request.py`. Live monday.com authorization, token refresh, and provider behavior require a user-controlled connection and remain unverified here.
