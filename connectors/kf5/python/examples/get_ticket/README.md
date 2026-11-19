# KF5 getTicket example

Run `uv run python -m examples.get_ticket.worker` after configuring the static `kf5` connection in Dex Web. Run `uv run pytest examples/get_ticket/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
