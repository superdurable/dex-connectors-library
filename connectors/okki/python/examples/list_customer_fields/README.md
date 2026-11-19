# Okki listCustomerFields example

Run `uv run python -m examples.list_customer_fields.worker` after configuring the static `okki` connection in Dex Web. Run `uv run pytest examples/list_customer_fields/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
