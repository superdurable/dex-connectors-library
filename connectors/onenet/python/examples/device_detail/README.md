# OneNET device detail Flow

Run `python -m examples.device_detail.worker` with a configured Dex project and `onenet` connection. The Flow reads device metadata by name. Run `uv run --group dev pytest -q examples/device_detail/integration_test.py` against a local Dex server for a real Worker and Client path with a fake provider.
