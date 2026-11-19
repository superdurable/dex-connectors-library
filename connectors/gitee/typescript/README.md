# Gitee TypeScript connector

The first TypeScript release implements the `getRepository` Query. Use the generated `newGetRepositoryStep` factory in a Dex Flow. The runnable [repository lookup example](examples/repository-lookup/README.md) uses a static connection named `gitee` and reads the provider token only through Dex Credentials.

The provider response can select `found`, `notFound`, `providerRejected`, `invalidResponse`, or `defect`. Temporary provider and transport failures retry. The checked-in Go conformance scenarios are replayed by `test/conformance.test.ts`; `test/integration/repository-lookup.integration.test.ts` runs the example through a real Dex Worker and Client with a local provider fixture.

From this directory, run `npm ci && npm run check`. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. The provider token is a secret; no live Gitee authorization was performed.
