# Amazon S3 Python connector

`dex-connector-amazon-s3` provides signed `listObjects`, `headObject`, `getObjectText`, and retry-safe `putObject` operations for Dex Flows. The generated Step factories and project connection bind the static `amazon-s3-report-archive` connection. See [the connector authorization guide](../README.md#authentication) for IAM, R2, and MinIO setup.

The [report archive example](examples/report_archive/README.md) uses all four operations. The Go-recorded conformance scenarios cover provider branches, retries, create-only read-back, and transport loss. Live Amazon S3, R2, and MinIO authorization and object behavior have not been verified for this Python package.

Start `dexcli dev` on unused local ports and set `DEX_FLOW_SERVICE_ADDRESS` to its Dex Server address. From `connectors/amazon/s3/python`, run `uv run python -m examples.report_archive.main`. In another terminal in that directory, run `uv run python -m examples.report_archive.start <unique-flow-id> examples/report_archive/input.example.json` to start a report.
