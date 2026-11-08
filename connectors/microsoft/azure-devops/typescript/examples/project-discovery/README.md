# Azure DevOps TypeScript project discovery Flow

This Flow calls `listProjects` with the static `azure-devops` Dex Web connection. It stores one bounded project page in a Dex Attribute and completes with the typed result. Configure the organization URL and PAT as described in the [connector README](../../../README.md).

With `dexcli dev` running and the connection configured, run `npm run example:worker --prefix connectors/microsoft/azure-devops/typescript`, then `npm run example:start --prefix connectors/microsoft/azure-devops/typescript`. The starter uses `{ "top": 20, "skip": 0 }`; increase `skip` by `top` to read another page. The Worker has no inbound listener. Optional branches are unwired and fail the Flow when selected.

For deterministic real-Dex verification, run `npm run test:integration --prefix connectors/microsoft/azure-devops/typescript` against `dexcli dev`. Live provider authorization remains unverified.
