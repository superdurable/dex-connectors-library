# Amazon S3 report archive

This runnable Flow writes a UTF-8 report with `putObject` in create-only mode, then calls `headObject`, `getObjectText`, and `listObjects`. The static Dex Web connection name is `amazon-s3-report-archive`.

Configure that connection in Dex Web **Connections** using the [authorization guide](../../../README.md#authentication). Set `DEX_FLOW_SERVICE_ADDRESS` to the local `dexcli dev` address, then run `uv run python -m examples.report_archive.main` from the Python package directory. Start a `AmazonS3ReportArchivePython` Flow with `bucket`, `key`, and `text`; the sample is [input.example.json](input.example.json).

The integration test uses a local S3 stand-in and a real Dex Worker and Client. It does not verify live Amazon S3, Cloudflare R2, or MinIO authorization or object behavior.
