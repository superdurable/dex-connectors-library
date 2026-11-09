# Box connector for TypeScript

`@superdurable/connector-box@0.1.0` implements the Go-verified `downloadFile` and `uploadFile` operations. Use the generated `newDownloadFileStep` and `newUploadFileStep` factories with the static `box` connection. The [connector README](../README.md) documents OAuth setup, scopes, limits, and branches.

`downloadFile` returns bounded Base64 content. `uploadFile` replaces an existing file by numeric ID, skips identical content, and reads back ambiguous outcomes, retrying when read-back cannot confirm the content. The [file-download example](examples/file-download/README.md) contains a runnable Worker and typed Flow.

Run `npm ci --prefix connectors/box/typescript` and `npm run check --prefix connectors/box/typescript` from the repository root. Provider behavior is replayed from `conformance/connectors/box/scenarios.json`. Live Box authorization remains unverified.
