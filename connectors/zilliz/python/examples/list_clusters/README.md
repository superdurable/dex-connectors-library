# Zilliz listClusters example

Run `uv run python -m examples.list_clusters.worker` after configuring the static `zilliz` connection in Dex Web. Run `uv run pytest examples/list_clusters/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
