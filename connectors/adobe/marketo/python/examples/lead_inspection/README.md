# Marketo Python lead inspection

This runnable Flow calls `getLead` through the static `adobe-marketo` connection, persists the lead profile, and completes. Optional branches are unwired and fail the Flow when selected. Configure `client_id` and secret `client_secret` using the [connection guide](../../../README.md); leave `access_token` blank for automatic renewal.

From `connectors/adobe/marketo/python`, run `uv run python -m examples.lead_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `MarketoLeadInspectionPython` with a unique Flow ID and input `{"leadId":318581}`.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
dexcli visualize examples/lead_inspection/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/adobe_marketo-python-fdg --open=false --python .venv/bin/python
```
