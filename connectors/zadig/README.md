# Zadig connector

[Zadig](https://koderover.com/terms) is operated by KodeRover (Shanghai) Information Technology Co., Ltd. This Go connector calls the official [Zadig v4.3 project-list API](https://docs.koderover.com/zadig/cn/Zadig%20v4.3/api/project/) to read one page of project keys, names, and deployment types. It does not start workflows or expose project configuration and secrets.

## Connection

Use an organization-operated Zadig deployment reachable through HTTPS. Its address is the Dex `baseUrl`; HTTP is accepted only for loopback tests. The [official API guide](https://docs.koderover.com/zadig/cn/Zadig%20v4.3/api/usage/) says to open the top-right user menu, choose **账号设置**, and copy the API Token. Enter it as the secret Dex `api_key` credential. Zadig's guide does not describe token scopes; use a user account whose project permissions are limited to read-only access. Leave timeout and response limit blank for displayed defaults. Rotate a compromised token in account settings.

## Go

The [project catalog Flow](go/examples/project-catalog/) is a runnable Worker with static Dex Web connection name `zadig`. Input `pageNum` and `pageSize` default to 1 and 20; `listed` completes the Flow. Optional branches fail unless an application wires a business response. Transient failures retry safely. The local mock integration uses synthetic project data and a real Dex Worker and Client. Live deployment authorization and project values remain unverified without a user-controlled connection.


## Python

The first Python package implements the read-only `listProjects` operation and replays all Go-recorded outcomes. See the [Python package](python/README.md) and its [runnable Flow](python/examples/project_list/README.md). Live provider authorization and responses remain unverified.

The [TypeScript package](typescript/) (v0.1.1) includes a [runnable Dex Flow](typescript/examples/basic/) and Go conformance replay.

## Java and Rust

Both packages implement `listProjects` and bind it to a Dex Query Step. The result lists projects visible to the connection in provider order. Zadig authenticates with the API token for the configured deployment. Use `pageNum` and `pageSize` to select a page. Zero requests the first page or the default size of 20, respectively.

The [Java Worker](java/examples/io/superdurable/dexconnectors/zadig/ProjectDirectoryWorker.java) serves `JavaZadigProjectDirectory`. From `java/`, run `./gradlew projectDirectoryWorker` with the deployment origin in `ZADIG_BASE_URL` and the API token supplied by your credential manager in `ZADIG_API_KEY`. Hosted Java applications can bind their loaded Dex project with `ProjectDirectory.fromProject(project, "zadig")`.

The [Rust Worker](rust/examples/list_projects.rs) serves `RustZadigProjectDirectory`. Set `DEX_CONNECTOR_CONFIG_FILE` to the connections file exported by Dex Web with the named `zadig` connection. From `rust/`, run `cargo run --example list_projects`. Both examples use `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server.

Generate the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/zadig/ProjectDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/zadig-java
dexcli visualize rust/examples/list_projects.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/zadig-rust
```
