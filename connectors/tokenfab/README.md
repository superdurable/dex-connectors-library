# TokenFab Model Catalog

`ListModels` reads model IDs from the [official TokenFab GET /v1/models API](https://www.tokenfab.cn/docs.html) over HTTPS with Bearer authentication. The [official privacy policy](https://www.tokenfab.cn/privacy.html) identifies 深圳象元工坊科技有限公司 as the Chinese operator. Output contains the provider model count and at most the first 50 IDs; inference, model metadata, and account data are excluded.

Sign in at the [TokenFab Console](https://www.tokenfab.cn/console), open 控制台 > API 密钥, create a `tk-` prefixed secret key, and copy it once into Dex Web Connections. The provider announced that its old `.com` API domain was retired; this connector uses `api.tokenfab.cn`. Leave timeout and maxResponseBytes blank for their displayed defaults.

The [runnable Dex example](go/examples/list-models/) uses a synthetic mock. Live account authorization and current model visibility have not been verified without a user-controlled provider key.

## Python

The [Python package](python/) exposes the same Query and branch contract as Go. Its [runnable example](python/examples/query/) uses static connection name `tokenfab`. The package replays Go-recorded provider scenarios and the example verifies retry and persistence with a real Dex Worker and Client. Live provider authorization has not been verified.

## TypeScript

`@superdurable/connector-tokenfab` provides the `listModels` Query and a [runnable TypeScript Dex example](typescript/examples/basic/README.md). The package replays Go-recorded provider scenarios with `npm run check`, and `npm run test:integration` verifies retry and persistence against `dexcli dev -open=false`. Real provider credentials and live API behavior have not been verified.

The [Java Worker](java/examples/io/superdurable/dexconnectors/tokenfab/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `TOKENFAB_API_KEY` in the Worker environment and use
the static connection name `tokenfab`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaTokenfabModelCatalog` with an empty JSON string or `RustTokenfabModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
