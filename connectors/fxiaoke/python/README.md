# FXiaoke Python connector

This package implements the `listObjectMetadata` Query. Configure the static `fxiaoke` connection with the app ID, app secret, and permanent code described in the [company README](../README.md). The connector exchanges app credentials for a short-lived token and returns only CRM object names, display names, and active flags. It exposes `found`, `providerRejected`, `invalidResponse`, and `defect` branches; only `found` is wired by the runnable example.

Run `uv sync --locked` and `uv run pytest` in this directory. The [CRM object metadata example](examples/list_object_metadata/) includes a Worker entrypoint and a real Dex integration test. It uses `dexcli dev -open=false` with a mock HTTP provider. Live FXiaoke authorization has not been verified.
