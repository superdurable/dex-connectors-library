# Amplitude Python user search

This runnable Flow calls `searchUsers` through the static `amplitude` connection, persists the returned user IDs, and completes. Optional branches are unwired and fail the Flow when selected. Configure `apiKey` and `secretKey` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/amplitude/python`, run `uv run python -m examples.user_search.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `AmplitudeUserSearchPython` with a unique Flow ID and input `{"user":"example-user"}`. Replace the sample with a user ID or prefix from Amplitude User Lookup.
