# Zhihuiya getPatentValuation example

Run `uv run python -m examples.get_patent_valuation.worker` after configuring the static `zhihuiya` connection in Dex Web. Run `uv run pytest examples/get_patent_valuation/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
