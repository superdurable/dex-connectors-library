# Youzan TypeScript connector

The first TypeScript release implements the `getShopBasic` Query. Use the generated `newGetShopBasicStep` factory in a Dex Flow. The runnable [shop lookup example](examples/shop-basic/README.md) uses a static connection named `youzan` and reads the provider token only through Dex Credentials.

The provider response can select `found`, `providerRejected`, `invalidResponse`, or `defect`. Temporary provider and transport failures retry. The checked-in Go conformance scenarios are replayed by `test/conformance.test.ts`; `test/integration/shop-basic.integration.test.ts` runs the example through a real Dex Worker and Client with a local provider fixture.

From this directory, run `npm ci && npm run check`. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. The provider token is a secret; no live Youzan authorization was performed.
