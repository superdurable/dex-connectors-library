# Google Cloud Storage connector for Python

The Python package implements `listObjects`, `downloadObject`, and `uploadObject` with the shared [connector manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and operation behavior. Runnable list, download, and upload Flows use the static `google-cloud-storage` Dex Web connection.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live Google authorization, Cloud Storage API calls, and Dex Web field rendering have not been verified in this checkout.
