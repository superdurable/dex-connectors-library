# Read Microsoft Teams thread replies

Create a Dex Web connection named `microsoft-teams-incident` using the [Microsoft Teams authorization guide](../../../README.md). From `python/`, run `uv sync --locked`, start `uv run python -m examples.list_thread_replies.worker`, then run `uv run python -m examples.list_thread_replies.start teams-replies-1 examples/list_thread_replies/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` if Dex uses another port. The Flow stores one bounded reply page in an Attribute. An unwired optional branch fails the Flow.

The integration test runs this Flow through a real Dex Worker and Client against a local Graph stand-in. Live Microsoft authorization and Graph calls remain unverified.
