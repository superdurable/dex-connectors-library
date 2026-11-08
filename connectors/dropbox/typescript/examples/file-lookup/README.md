# Dropbox TypeScript metadata Flow

This runnable Flow calls `getMetadata` through the static `dropbox` connection and completes with the selected file or folder identity. Unwired optional branches fail the Flow.

From `connectors/dropbox/typescript`, generate a strict FDG 2.0 graph:

```bash
dexcli visualize examples/file-lookup/flow.ts --schema-version 2.0 --lint app --json --out /tmp/dropbox-ts-metadata-fdg --open=false
```

Dex CLI 1.37 writes a partial graph and reports `dynamic_step_target` for the connector factory Step. The same diagnostic occurs in existing TypeScript connector examples; the Worker integration check below exercises the complete Flow.

Configure the `dropbox / dropbox` OAuth connection in Dex Web as described in the [connector README](../../../README.md), then run `npm run example:worker`. Start `DropboxGetMetadataTypeScript` with a unique Flow ID and `{"path":"/example.txt"}`.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8983 npm run example:integration`. Live Dropbox authorization remains unverified.
