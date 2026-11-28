# Miyang Model Catalog

`ListModels` reads model IDs from the [official Miyang Gateway GET /api/v1/models API](https://miyang.cn/model/docs) over HTTPS with Bearer authentication. The [official terms](https://miyang.cn/en/terms) identify 米羊科技（上海）有限公司 as the Chinese service operator. Output contains the provider model count and at most the first 50 IDs; inference, model metadata, and account data are excluded.

Sign in at [Miyang Gateway](https://miyang.cn/model), open API Keys in the console, and create or copy a secret `miyang-` key into Dex Web Connections. The model-list endpoint is public, but a key can reveal internal or whitelisted models available to that account. The provider notes that `miyang/*` models are for its Alice client; use models marked `general` for other apps. Leave timeout and maxResponseBytes blank for their displayed defaults.

The [runnable Dex example](go/examples/list-models/) uses a synthetic mock. Live account authorization and current model visibility have not been verified without a user-controlled provider key.

The [Python listModels example](python/examples/) provides a runnable Dex Worker and local conformance replay.

The [TypeScript listModels example](typescript/examples/basic/) runs a Dex Worker and Client, with Go-recorded provider scenarios replayed by the package tests.

The [Java Worker](java/examples/io/superdurable/dexconnectors/miyang/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `MIYANG_API_KEY` in the Worker environment and use
the static connection name `miyang`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaMiyangModelCatalog` with an empty JSON string or `RustMiyangModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
