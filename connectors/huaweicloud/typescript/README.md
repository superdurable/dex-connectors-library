# Huawei Cloud TypeScript connector

The first TypeScript release implements the `listAccessibleProjects` Query. Use the generated `newListAccessibleProjectsStep` factory in a Dex Flow. The runnable [IAM project list example](examples/project-list/README.md) uses a static connection named `huaweicloud` and persists only project metadata.

The provider response can select `found`, `providerRejected`, `invalidResponse`, or `defect`. Temporary token and project request failures retry. The checked-in Go conformance scenarios are replayed by `test/conformance.test.ts`; `test/integration/project-list.integration.test.ts` runs the example through a real Dex Worker and Client with a local provider fixture.

From this directory, run `npm ci && npm run check`. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. IAM credentials and tokens are secrets; no live Huawei Cloud authorization was performed.
