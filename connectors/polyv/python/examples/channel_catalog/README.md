# Polyv channel catalog Flow

Run `python -m examples.channel_catalog.worker` with a configured Dex project and `polyv` connection. The Flow reads the signed channel list and stores its IDs. Run `uv run --group dev pytest -q examples/channel_catalog/integration_test.py` against a local Dex server for a real Worker and Client path with a fake provider.
