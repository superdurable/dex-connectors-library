# Dropbox connector for TypeScript

This package implements `listFolder`, `getMetadata`, `continueFolder`, `downloadFile`, and `uploadFile` with the Go-verified request and branch contracts. See the [connector README](../README.md) for Dropbox OAuth setup, scopes, and response limits.

Use `newProjectConnection(project, "dropbox")` with operation-specific Step factories. The [metadata Flow](examples/file-lookup/README.md) shows a runnable Dex Worker. `downloadFile` returns bounded Base64 content. `uploadFile` accepts Base64 content, overwrites one path with the same bytes on retry, and returns only the provider file identity.

From the repository root, run `npm ci --prefix connectors/dropbox/typescript` and `npm run check --prefix connectors/dropbox/typescript`. The conformance test replays `conformance/connectors/dropbox/scenarios.json`; live Dropbox authorization remains unverified.
