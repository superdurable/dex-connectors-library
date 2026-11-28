# ZenTao

The Go connector lists products visible to a ZenTao REST v1 account. `ListProducts` calls [GET `/api.php/v1/products`](https://www.zentao.net/book/api/675.html) and returns product IDs, names, codes, types, access controls, and the reported total. It omits owners, creators, and whitelist members from the provider response. The official v1 endpoint does not document pagination, so this operation retrieves its single response and rejects more than 1,000 products or an oversized response.

This connector requires ZenTao 16.5 or later. In Dex Web, enter the HTTPS root of your own installation, including `/zentao` if your login URL contains that subpath. A self-hosted instance has no universal endpoint. Obtain the token in a trusted client by POSTing JSON `account` and `password` to `<installation-root>/api.php/v1/tokens`, then copy only the returned `token` into the secret connection field. The connector never stores the account password or returns the token in Flow data or errors. See the [official setup guide](https://www.zentao.net/book/api/1397.html) and [token endpoint](https://www.zentao.net/book/api/664.html).

The account needs permission to view products. The [runnable Dex Flow](go/examples/product-list/README.md) shows the local mock path. Live ZenTao instance authorization and provider response remain unverified.

The logo uses the official [ZenTao website footer asset](https://static.zentao.net/web/data/source/zentao/default/wide/logo-footer.png) embedded unchanged in SVG.

## Python

The first Python package implements the read-only product list and replays the Go-recorded provider outcomes. See the [Python package](python/README.md) and its [runnable Flow](python/examples/product_list/README.md). Live ZenTao authorization and responses remain unverified.

The [TypeScript package](typescript/) (v0.1.0) includes a [runnable Dex Flow](typescript/examples/basic/) and Go conformance replay.

The connection URL is the installation root, including any subpath. The connector adds `/api.php/v1/products` and returns an empty string for an omitted or null product code.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/zentao/ProductDirectoryWorker.java) registers `JavaZenTaoProductDirectory`. Run `./gradlew productDirectoryWorker` from `java/` with `ZENTAO_TOKEN` and the installation root in `ZENTAO_BASE_URL` supplied through your credential manager. Hosted Java applications can use `ProductDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_products.rs) registers `RustZenTaoProductDirectory`. From `rust/`, run `cargo run --example list_products` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `zentao` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Start the Flow with an empty JSON object.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/zentao/ProductDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/zentao-java
dexcli visualize rust/examples/list_products.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/zentao-rust
```
