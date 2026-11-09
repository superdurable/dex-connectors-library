# Shortcut Python story inspection

This runnable Flow calls `getStory` through the static `shortcut` connection, persists selected Story metadata, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_token` and leave `baseUrl` blank for the public API in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/shortcut/python`, run `uv run python -m examples.story_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `ShortcutStoryInspectionPython` with a unique Flow ID and input `{"storyId":"12345"}`. Replace the sample ID with a real Story ID.

For strict FDG 2.0 verification, run:

```bash
dexcli visualize examples/story_inspection/flow.py --language python --python .venv/bin/python --schema-version 2.0 --lint app --json --out /tmp/shortcut-python-fdg --open=false
```

The integration test exercises a real Dex Worker and Client against a local provider fake and retries a 503 before persisting the response.
