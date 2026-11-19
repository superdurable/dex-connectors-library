# Apifox project-summary Flow

Install with `npm ci --prefix connectors/apifox/typescript` from the repository
root. Configure the `apifox` connection in Dex Web Connections with a personal
access token and find a positive decimal project ID in Apifox project settings.
With `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev`, run `npm run
example:worker` and, in another shell, `npm run example:start -- 123` from the
TypeScript package directory. The checked-in entrypoints are
[`worker.ts`](worker.ts) and [`start.ts`](start.ts). Only the `found` branch is
wired; other outcomes fail the Flow for inspection.

The integration test exercises this Flow through real Dex with a fake provider.
Provider authorization and live project exports remain unverified.

For strict FDG 2.0 generation, build the package and make its package import
visible to the analyzer from `connectors/apifox/typescript`:

```bash
npm run build
npm install --prefix examples/project-summary --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/project-summary/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/apifox-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
