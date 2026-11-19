# Amap address lookup Flow

Install with `npm ci --prefix connectors/amap/typescript` from the repository root. Configure the `amap` Dex Web Connection with a secret Web Service Key using [Amap Application Management](https://console.amap.com/dev/key/app). With `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev`, run `npm run example:worker` and, in another shell, `npm run example:start -- '北京市朝阳区阜通东大街6号' 北京` from the TypeScript package directory. The checked-in entrypoints are [worker.ts](worker.ts) and [start.ts](start.ts). Only the `geocoded` branch is wired; other outcomes fail the Flow for inspection.

The integration test exercises this Flow through a real Dex Worker and Client with a fake provider, including retry after HTTP 503. A live Web Service Key and provider response are not verified.

For strict FDG 2.0 generation, build the package and make its package import visible to the analyzer:

```bash
npm run build
npm install --prefix examples/address-lookup --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/address-lookup/flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --typescript-compiler node_modules/typescript
```

The FDG must report `valid: true` with no diagnostics.
