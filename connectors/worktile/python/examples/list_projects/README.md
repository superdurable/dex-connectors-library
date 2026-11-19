# Worktile listProjects example

Run `uv run python -m examples.list_projects.worker` after configuring the static `worktile` connection in Dex Web. Run `uv run pytest examples/list_projects/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
