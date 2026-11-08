# PubSub connector for Python

This package implements `listTopics` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup. The [runnable topic Flow](examples/topics/README.md) uses the static `google-pubsub` Dex Web connection and requires a project ID in its start input.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live PubSub authorization and Dex Web field rendering have not been verified.
