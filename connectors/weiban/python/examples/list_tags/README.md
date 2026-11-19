# Weiban listTags example

Run `uv run python -m examples.list_tags.worker` after configuring the static `weiban` connection in Dex Web. Run `uv run pytest examples/list_tags/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
