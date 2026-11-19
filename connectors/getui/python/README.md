# Getui Python connector

This package implements the `getDailyPushReport` Query. Configure the static `getui` connection with the app ID, app key, and master secret described in the [company README](../README.md). The connector exchanges a signed request for a short-lived token and returns only prior-day aggregate push counts. It exposes `found`, `providerRejected`, `invalidResponse`, and `defect` branches; only `found` is wired by the runnable example.

Run `uv sync --locked` and `uv run pytest` in this directory. The [daily push report example](examples/get_daily_push_report/) includes a Worker entrypoint and a real Dex integration test. It uses `dexcli dev -open=false` with a mock HTTP provider. Live Getui authorization has not been verified.
