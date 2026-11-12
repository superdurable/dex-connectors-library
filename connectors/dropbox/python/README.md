# Dropbox connector for Python

`dex-connector-dropbox==0.2.0` implements the five Go-verified Dropbox operations: `listFolder`, `getMetadata`, `continueFolder`, `downloadFile`, and `uploadFile`. Use the generated `new_*_step` factories with the static `dropbox` connection. See the [connector README](../README.md) for the provider application, OAuth scopes, limits, and failure branches.

`downloadFile` returns bounded Base64 content. `uploadFile` accepts Base64 content, overwrites one absolute path with the same bytes on retry, and returns file identity. Optional branches fail the Flow unless wired explicitly. The [file transfer example](examples/file_transfer/README.md) provides a runnable Worker and typed Flow.

Run `uv sync --locked --project connectors/dropbox/python`, `uv run --project connectors/dropbox/python pytest connectors/dropbox/python/tests`, and `uv run --project connectors/dropbox/python mypy --strict connectors/dropbox/python/src` from the repository root. Provider behavior is replayed from `conformance/connectors/dropbox/scenarios.json`. Live Dropbox authorization remains unverified.
