# Creatio Python contact inspection

This runnable Flow calls `getContact` through the static `creatio` connection, persists the contact profile, and completes. Optional branches are unwired and fail the Flow when selected. Configure `client_id` and secret `client_secret` using the [connection guide](../../../README.md); enter `baseUrl` and `tokenEndpoint`; leave `access_token` blank for automatic renewal.

From `connectors/creatio/python`, run `uv run python -m examples.contact_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `CreatioContactInspectionPython` with a unique Flow ID and input `{"contactId":"90a68db5-cb3a-4f64-bb60-c0a934ee7600"}`.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
dexcli visualize examples/contact_inspection/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/creatio-python-fdg --open=false --python .venv/bin/python
```
