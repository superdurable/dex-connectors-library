# Box connector for Python

`dex-connector-box==0.1.0` implements the Go-verified `downloadFile` and `uploadFile` operations. Use the generated `new_download_file_step` and `new_upload_file_step` factories with the static `box` connection. The [connector README](../README.md) documents OAuth setup, Box scopes, size limits, and failure branches.

`downloadFile` returns bounded Base64 content. `uploadFile` replaces an existing file's current content by numeric file ID, skips an already identical file, and reads back ambiguous outcomes before reporting uncertainty. Optional branches fail the Flow unless wired explicitly. The [file-download example](examples/file_download/README.md) provides a runnable Worker and typed Flow.

Run `uv sync --locked --project connectors/box/python`, `uv run --project connectors/box/python pytest connectors/box/python/tests`, and `uv run --project connectors/box/python mypy --strict connectors/box/python/src` from the repository root. Provider behavior is replayed from `conformance/connectors/box/scenarios.json`. Live Box authorization remains unverified.
