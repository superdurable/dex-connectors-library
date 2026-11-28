# Tingyun connector

This connector reads the monitored mobile App list through the [official Tingyun App API](https://wukongdoc.tingyun.com/app/api/basic.html). It calls `GET <platform origin>/app-api/app/list` and returns only each application's numeric ID and name. The provider response includes `appToken`; the connector excludes it from durable results and receipts.

The [official API overview](https://wukongdoc.tingyun.com/app/api/) documents an HTTPS platform host such as `https://wukong2.tingyun.com`, and the `authorization: token <accessToken>` header. Sign in to your Tingyun platform, hover over the lower-left account name, open **账户管理 > API**, and generate an accessToken. Enter its platform origin and the secret token in Dex Web Connections. The connector accepts only HTTPS `*.tingyun.com` hosts and disables redirects; private hosts are outside this version.

The [runnable Flow](go/examples/list-applications/README.md) uses the static `tingyun` connection. Real accessToken authorization and Dex Web form interaction remain unverified.

## Python

The [Python package](python/) exposes the same Query and branch contract as Go. Its [runnable example](python/examples/query/) uses static connection name `tingyun`. The package replays Go-recorded provider scenarios and the example verifies retry and persistence with a real Dex Worker and Client. Live provider authorization has not been verified.

## TypeScript

`@superdurable/connector-tingyun` provides the `listApplications` Query and a [runnable TypeScript Dex example](typescript/examples/basic/README.md). The package replays Go-recorded provider scenarios with `npm run check`, and `npm run test:integration` verifies retry and persistence against `dexcli dev -open=false`. Real provider credentials and live API behavior have not been verified.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/tingyun/ApplicationDirectoryWorker.java) registers `JavaTingyunApplicationDirectory`. Run `./gradlew applicationDirectoryWorker` from `java/` with `TINGYUN_ACCESS_TOKEN` supplied through your credential manager. Set `TINGYUN_API_HOST` to the configured provider origin. Hosted Java applications can use `ApplicationDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_applications.rs) registers `RustTingyunApplicationDirectory`. From `rust/`, run `cargo run --example list_applications` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `tingyun` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Start the Flow with an empty JSON object.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/tingyun/ApplicationDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/tingyun-java
dexcli visualize rust/examples/list_applications.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/tingyun-rust
```
