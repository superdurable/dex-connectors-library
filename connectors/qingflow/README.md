# Qingflow connector

This connector calls the current [Qingflow OpenAPI application list](https://help-center.qingflow.com/docs/outline/jhzcl2q96h/), `GET https://api.qingflow.com/app`, and returns only `appKey` and `appName`. It does not read application records, form contents, or members.

In the target workspace, open [轻流](https://qingflow.com/) **轻商城 > 插件中心 > 第三方连接 > OPEN API > 配置** and copy the secret super-admin `accessToken` into Dex Web Connections. The [official API documentation](https://help-center.qingflow.com/docs/outline/jhzcl2q96h/) says permission-group tokens cannot call this list endpoint. The [plugin guide](https://help-center.qingflow.com/docs/outline/2iztdeizlf/) lists OPEN API under the Enterprise version. The token is sent only in the HTTPS `accessToken` header, never in Flow input.

The public-cloud endpoint is fixed; DingTalk and private Qingflow deployments have different hosts. Run the [example Flow](go/examples/list-applications/) with the generated mock or a user-controlled connection. Real Qingflow authorization and plan entitlement remain to be tested with an account.

The [Python package](python/) implements the same `listApplications` Query and replays the seven Go-recorded provider scenarios. Its [runnable Flow](python/examples/application_directory/) verifies retry and persistence with a real local Dex Worker and Client.
The [TypeScript package](typescript/) provides the same `ListApplications` operation, recorded conformance, and a runnable Dex Worker and Client example.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/qingflow/ApplicationDirectoryWorker.java) registers `JavaQingflowApplicationDirectory`. Run `./gradlew applicationDirectoryWorker` from `java/` with `QINGFLOW_ACCESS_TOKEN` supplied through your credential manager. Hosted Java applications can use `ApplicationDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_applications.rs) registers `RustQingflowApplicationDirectory`. From `rust/`, run `cargo run --example list_applications` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `qingflow` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Start the Flow with an empty JSON object.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/qingflow/ApplicationDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/qingflow-java
dexcli visualize rust/examples/list_applications.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/qingflow-rust
```
