# PostHog Python saved insights

This runnable Flow reads one insight page through the static `posthog`
connection. Configure a personal API key with `insight:read` in Dex Web, then run
`uv run python -m examples.insight_list.worker` from `python/` with
`DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set. Start
`PostHogPythonInsightList` with input such as:

```json
{"projectId":42,"offset":0,"limit":20}
```

Generate strict FDG 2.0 with
`dexcli visualize examples/insight_list/flow.py --schema-version 2.0 --json --python .venv/bin/python --out /tmp/posthog-python-insights-fdg --lint app`.
Live PostHog authorization remains unverified.
