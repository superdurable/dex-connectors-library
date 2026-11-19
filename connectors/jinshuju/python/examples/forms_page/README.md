# Jinshuju listFormsPage example

Run `uv run python -m examples.forms_page.worker` after configuring the static `jinshuju` connection in Dex Web. Run `uv run pytest examples/forms_page/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
