# Personio Python person directory

This runnable Flow calls `listPersons` through the static `personio` connection, persists a filtered page with its next cursor, and completes. Optional branches are unwired and fail the Flow when selected. Configure `client_id` and secret `client_secret` using the [connection guide](../../../README.md); leave `access_token` blank for automatic renewal.

From `connectors/personio/python`, run `uv run python -m examples.person_directory.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `PersonioPersonDirectoryPython` with a unique Flow ID and input `{"limit":10,"status":"ACTIVE"}`.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
dexcli visualize examples/person_directory/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/personio-list-python-fdg --open=false --python .venv/bin/python
```
