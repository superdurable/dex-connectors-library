# Useasy listAgents example

Run `uv run python -m examples.list_agents.worker` after configuring the static `useasy` connection in Dex Web. Run `uv run pytest examples/list_agents/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
