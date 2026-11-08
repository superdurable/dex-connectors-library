# Shortcut TypeScript story inspection

This runnable Flow calls `getStory` through the static `shortcut` connection, persists the selected fields, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_token` and leave `baseUrl` blank for the public API in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/shortcut/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <story-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

For strict FDG 2.0 verification, build the package and make its own package import visible to the analyzer:

```bash
npm run build
npm install --prefix examples/story-inspection --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/story-inspection/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/shortcut-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
