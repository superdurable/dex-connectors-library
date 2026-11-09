# Creatio connector for Python

Implements `getContact` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for Identity Service setup and renewal. The [runnable Contact Flow](examples/contact_inspection/README.md) uses the static `creatio` connection.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests --ignore=tests/integration` from this directory. Live Creatio authorization remains unverified.
