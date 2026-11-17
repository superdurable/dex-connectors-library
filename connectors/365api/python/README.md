# 365 API Python connector

`dex-connector-365api` exposes the `listModels` Query Step at `dex_connectors._365api`. See the [365 API developer center](https://www.hby365.studio/developers) for the documented `GET /v1/models` endpoint and API Key setup.

The [runnable Flow](examples/model_list/) uses the static `hby365` connection and stores only model IDs. Its integration test exercises a real Dex Worker/Client with a fake provider. Live 365 API authorization and model availability remain unverified.
