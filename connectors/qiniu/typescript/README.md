# Qiniu TypeScript connector

The first TypeScript release implements the `listBuckets` Query. Use the generated `newListBucketsStep` factory in a Dex Flow. The runnable [bucket list example](examples/bucket-list/README.md) uses a static connection named `qiniu`; it never persists signing credentials.

The provider response can select `listed`, `providerRejected`, `invalidResponse`, or `defect`. Temporary provider and transport failures retry. The checked-in Go conformance scenarios are replayed by `test/conformance.test.ts`; `test/integration/bucket-list.integration.test.ts` runs the example through a real Dex Worker and Client with a local provider fixture.

From this directory, run `npm ci && npm run check`. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. The AccessKey and SecretKey are secrets; no live Qiniu authorization was performed.
