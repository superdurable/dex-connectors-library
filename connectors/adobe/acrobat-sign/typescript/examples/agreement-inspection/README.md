# Adobe Acrobat Sign TypeScript agreement inspection

This runnable Flow calls `getAgreement` through the static `adobe-acrobat-sign` connection and persists the selected status. Optional branches are unwired and fail the Flow when selected. Configure `integration_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/adobe/acrobat-sign/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <agreement-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
