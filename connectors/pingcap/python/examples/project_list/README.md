# PingCAP project list Flow

Run `python -m examples.project_list.worker` with a configured Dex project and `pingcap` connection. The Flow reads a page of TiDB Cloud projects. Run `uv run --group dev pytest -q examples/project_list/integration_test.py` against a local Dex server for a real Worker and Client path with a fake provider.
