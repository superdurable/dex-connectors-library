# FluRouter connector

This connector lists the models visible to the current Key through [FluRouter's model discovery API](https://flurouter.net/platform-api.html), `GET https://flurouter.net/v1/models`. It returns only `data[].id` strings, without provider metadata. The list reflects account, organization, and Key permissions; it may differ from the public catalog. FluRouter (川流 Token) is operated by [河南天诺信息技术有限公司](https://flurouter.net/).

Follow the [access guide](https://flurouter.net/guide): sign in to the console, open **API Keys**, create a Key with the needed model permissions, and copy its `sk-il-` value when shown. Save it in Dex Web Connections. The Query takes `{}` and sends the Key only as a Bearer header over HTTPS. The connector disables redirects, bounds the response, and keeps raw provider errors out of Flow output and failures.

Run the [example Flow](go/examples/list-models/) with the generated mock or a user-controlled connection. The live Key-visible model list is unverified without a user-controlled Key.

The [Python package](python/README.md) includes a [runnable Dex Flow](python/examples/model_catalog/README.md) and replays the Go-recorded provider scenarios. Live provider authorization remains unverified without user-controlled credentials.

The [TypeScript package](typescript/README.md) includes a [runnable Dex Flow](typescript/examples/basic/README.md) and replays the Go-recorded provider scenarios. Live provider authorization remains unverified without user-controlled credentials.

The [Java Worker](java/examples/io/superdurable/dexconnectors/flurouter/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `FLUROUTER_API_KEY` in the Worker environment and use
the static connection name `flurouter`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaFlurouterModelCatalog` with an empty JSON string or `RustFlurouterModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
