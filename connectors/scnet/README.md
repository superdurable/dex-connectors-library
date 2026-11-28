# SCNet Model API connector

`ListModels` reads IDs available to a SCNet API Key. The official [Models API reference](https://www2.scnet.cn/help/docs/mainsite/mass/api/modulequery.html) documents `GET /api/llm/v1/models`, Bearer authentication, and the OpenAI-compatible `object: list` / `data[].id` response. Output contains the provider model count and at most the first 50 IDs. Inference, model metadata and account data are excluded.

## Authorization

Sign in to the [SCNet console](https://www.scnet.cn/ui/console/index.html), open Model API > API Keys, create a general Key, set its team, name, service type and permitted models, and save the one-time secret into Dex Web Connections. The [official key guide](https://www2.scnet.cn/help/docs/mainsite/mass/tutorial/apikeymgt.html) explains the exact path and options. Leave timeout and maximum response size blank for the displayed defaults.

## Example

The [runnable Flow](go/examples/list-models/) starts with `{}` and completes with a typed `ListModelsOutput`. Its default mock returns one model. Live SCNet authorization, model visibility and provider response variation remain unverified without a user-controlled Dex Web Connection.

The [Python package](python/) implements `listModels` and its [runnable Flow](python/examples/current_query/) verifies retry and persistence with a real local Dex Worker and Client.
The [TypeScript package](typescript/) provides the same `ListModels` operation, a Go-recorded conformance suite, and a runnable Dex Worker and Client example.

The [Java Worker](java/examples/io/superdurable/dexconnectors/scnet/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `SCNET_API_KEY` in the Worker environment and use
the static connection name `scnet`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaScnetModelCatalog` with an empty JSON string or `RustScnetModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
