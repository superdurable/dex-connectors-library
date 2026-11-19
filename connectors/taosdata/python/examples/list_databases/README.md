# TDengine Cloud listDatabases example

Run `uv run python -m examples.list_databases.worker` after configuring the static `taosdata` connection in Dex Web. Run `uv run pytest examples/list_databases/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
