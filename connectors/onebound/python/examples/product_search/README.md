# Onebound product search Flow

Run `python -m examples.product_search.worker` with a configured Dex project and `onebound` connection. The Flow searches public 1688 listings by keyword. Run `uv run --group dev pytest -q examples/product_search/integration_test.py` against a local Dex server for a real Worker and Client path with a fake provider.
