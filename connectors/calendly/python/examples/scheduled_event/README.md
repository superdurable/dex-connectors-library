# Scheduled event lookup

This runnable Python Flow reads one Calendly scheduled event and saves the safe event result. Configure a Dex Web connection named `calendly-scheduling` using either a personal access token or Calendly OAuth. The connection's authorization guide links to Calendly's token and OAuth application pages and lists the scopes.

Start a local Dex stack with `dexcli dev -dex-port 40850 -web-port 40851 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40850 uv run python -m examples.scheduled_event.worker` from `connectors/calendly/python`. In another terminal, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40850 uv run python -m examples.scheduled_event.start <unique-flow-id> https://api.calendly.com/scheduled_events/<id>`.

Generate a strict FDG 2.0 graph from `connectors/calendly/python` with `dexcli visualize ./examples/scheduled_event/flow.py --language python --python .venv/bin/python --schema-version 2.0 --json --out ./build/calendly-scheduled-event`. The sole provider Query exposes the `found` branch; optional rejection branches fail the Flow when selected.
