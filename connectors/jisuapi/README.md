# JisuAPI connector

This connector calls the official [administrative-area province API](https://www.jisuapi.com/api/area/), `GET https://api.jisuapi.com/area/province`, and returns only province IDs and names. JisuAPI is operated by [Hangzhou Jisu Internet Technology Co., Ltd.](https://www.jisuapi.com/about/).

Register at [极速数据](https://www.jisuapi.com/), apply for 全国省市县行政区划分, and copy the secret `appkey` from 个人中心 into Dex Web Connections. The [official integration guide](https://m.jisuapi.com/news/detail/580) also explains optional IP whitelisting. The provider requires `appkey` in the HTTPS query string; this connector disables redirects and excludes the URL, key, and raw response message from results and errors.

The official product page lists 100 free calls per day for free members, with higher membership allowances. A successful status 0 generally counts as a call according to the integration guide. Run the [example Flow](go/examples/list-provinces/) with the generated mock or a user-controlled connection.

# TypeScript

The TypeScript package `@superdurable/connector-jisuapi` implements `listProvinces`. Its runnable Flow is in [`typescript/examples/basic`](typescript/examples/basic); the conformance test replays Go-recorded provider responses. Live JisuAPI authorization remains unverified.

The [Python package](python/) mirrors the Go Query and includes a [runnable Dex Worker/Client Flow](python/examples/provinces/) and Go scenario replay. Real provider credentials and live API behavior remain unverified.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/jisuapi/ProvinceDirectoryWorker.java) registers `JavaJisuAPIProvinceDirectory`. Run `./gradlew provinceDirectoryWorker` from `java/` with `JISUAPI_APPKEY` supplied through your credential manager. Hosted Java applications can use `ProvinceDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_provinces.rs) registers `RustJisuAPIProvinceDirectory`. From `rust/`, run `cargo run --example list_provinces` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `jisuapi` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Start the Flow with an empty JSON object.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/jisuapi/ProvinceDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/jisuapi-java
dexcli visualize rust/examples/list_provinces.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/jisuapi-rust
```
