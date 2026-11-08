# Ashby Python candidate inspection

This runnable Flow calls `getCandidate` through the static `ashby` connection, persists the selected candidate profile, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/ashby/python`, run `uv run python -m examples.candidate_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `AshbyCandidateInspectionPython` with a unique Flow ID and input `{"candidateId":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e"}`. Replace the sample candidate ID with one returned by Ashby.
