# Greenhouse Python candidate inspection

This runnable Flow calls `getCandidate` through the static `greenhouse` connection, persists the candidate profile, and completes. Optional branches are unwired and fail the Flow when selected. Configure `client_id` and secret `client_secret` using the [connection guide](../../../README.md); leave `access_token` blank for automatic renewal.

From `connectors/greenhouse/python`, run `uv run python -m examples.candidate_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `GreenhouseCandidateInspectionPython` with a unique Flow ID and input `{"candidateId":53883394}`.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
dexcli visualize examples/candidate_inspection/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/greenhouse-python-fdg --open=false --python .venv/bin/python
```
