# Youdao translate_text example

Run `uv run python -m examples.translate_text.worker` after configuring the static `youdao` connection in Dex Web. Run `uv run pytest examples/translate_text/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
