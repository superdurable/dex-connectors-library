# Zilliz Cloud connector

This connector lists clusters accessible to a Zilliz Cloud API Key using the official [List Clusters V2](https://docs.zilliz.com/reference/restful/list-clusters-v2) control-plane API. `ListClusters` reads the first page of at most 25 clusters and returns only ID, name, region, plan, and status. It does not expose cluster endpoints or vector data. The [Zilliz China website](https://zilliz.com.cn/terms-and-conditions) is operated by 上海赜睿信息科技有限公司.

## Connection

Sign in at [Zilliz Cloud](https://cloud.zilliz.com), open organization > API Keys > + API Key, and create a customized Key with project Read-Only permission for the intended clusters. [API Key guidance](https://docs.zilliz.com/docs/manage-api-keys) explains role restrictions. Copy the Key into Dex Web Connections. Leave timeout and maximum response size blank for the displayed defaults.

Run the [example Flow](go/examples/list-clusters/) with `{}` input. The checked-in mock returns one cluster. Live Zilliz authorization and cluster behavior require a user-provided Key and have not been verified here.

## Python

The `python/` package provides the `listClusters` Query. Run its checked-in example from `python/examples/`.

The [TypeScript package](typescript/README.md) implements `listClusters` with a [runnable Dex Flow](typescript/examples/cluster-list/README.md). Its provider scenarios replay the Go-recorded contract; live authorization remains unverified without user-controlled credentials.

Cluster metadata uses UTF-8 byte limits. Missing or null names, regions, plans, and statuses appear as empty strings.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/zilliz/ClusterDirectoryWorker.java) registers `JavaZillizClusterDirectory`. Run `./gradlew clusterDirectoryWorker` from `java/` with `ZILLIZ_API_KEY` supplied through your credential manager. Hosted Java applications can use `ClusterDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_clusters.rs) registers `RustZillizClusterDirectory`. From `rust/`, run `cargo run --example list_clusters` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `zilliz` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Start the Flow with an empty JSON object.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/zilliz/ClusterDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/zilliz-java
dexcli visualize rust/examples/list_clusters.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/zilliz-rust
```
