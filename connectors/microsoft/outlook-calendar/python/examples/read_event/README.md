# Read an Outlook event

`ReadOutlookEventFlow` consumes `GetEventInput` and returns the matching `Event`. The Worker loads the static `outlook-calendar-scheduler` connection through the generated project binding. The connection may use delegated Microsoft OAuth or app-only Entra credentials.

From `connectors/microsoft/outlook-calendar/python`, install with `uv sync --all-groups`, run `dexcli dev -dex-port 40720 -web-port 40721 -open=false`, then run `DEX_SERVER_ADDRESS=localhost:40720 uv run python -m examples.read_event.worker`. Open Dex Web at `http://127.0.0.1:40721`, configure the named connection using its authorization guide, and start the Flow with an event ID from that mailbox.

Run `DEX_SERVER_ADDRESS=localhost:40720 uv run pytest examples/read_event/integration_test.py -q` for the real Dex Worker and Client test using an inert Graph response. Generate the strict FDG 2.0 graph with `dexcli visualize examples/read_event/flow.py --schema-version 2.0 --lint app --json --out /tmp/outlook-read-fdg --python .venv/bin/python`.
