# PostHog Python event capture

This runnable Flow captures one event through the static `posthog` connection.
Configure the project token in Dex Web, then run
`uv run python -m examples.event_capture.worker` from `python/` with
`DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set. Start
`PostHogPythonEventCapture` with input such as:

```json
{"event":"Signed Up","distinctId":"member-1","timestamp":"2026-10-29T00:00:00Z"}
```

Generate strict FDG 2.0 with
`dexcli visualize examples/event_capture/flow.py --schema-version 2.0 --json --python .venv/bin/python --out /tmp/posthog-python-fdg --lint app`.
The integration test runs this Flow through a real Dex Worker and Client using
the generated mock. Live PostHog authorization and ingestion remain unverified.
