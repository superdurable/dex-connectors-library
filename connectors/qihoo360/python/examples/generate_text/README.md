# Qihoo360 generateText example

Run `uv run python -m examples.generate_text.worker` after configuring the static `qihoo360` connection in Dex Web. Run `uv run pytest examples/generate_text/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
