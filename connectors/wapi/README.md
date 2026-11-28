# Wapi vehicle brand prices

`ListVehicleBrands` reads brand names, IDs, and maintenance-record query prices in Chinese fen from the [official brand and price API](https://www.wapi.cn/api_detail/170/324.html?format=md). [Wapi's about page](https://www.wapi.cn/help_42.html) identifies 厦门训数科技有限公司 as the Chinese operator. This connector calls the documented read-only, unbilled subinterface and never orders maintenance records.

At [Wapi](https://www.wapi.cn), open 会员中心 > 我的应用, create an app, add 维修保养记录精准版, and copy its numeric AppID and 32-character secret to Dex Web Connections. Select MD5 signature verification and authorize your egress IP if required. The connector signs `appid`, `format=json`, and a Unix timestamp in seconds; the secret is excluded from the Flow. Leave timeout and maxResponseBytes blank for their displayed defaults.

The [runnable vehicle-brand Flow](go/examples/vehicle-brands/) uses `ConnectionName = "wapi"`. Run its worker with `go run .`, then start `WapiVehicleBrands` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow. Real provider authorization is unverified without a customer-controlled app.

The [Python package](python/README.md) includes a runnable Dex Worker and Client example and replays the Go-recorded provider scenarios.

Wapi reports prices in fen. An omitted or null price is returned as zero. The result includes up to 200 validated brands and the total count.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/wapi/VehicleBrandDirectoryWorker.java) registers `JavaWapiVehicleBrandDirectory`. Run `./gradlew vehicleBrandDirectoryWorker` from `java/` with `WAPI_APP_SECRET` and the application identifier in `WAPI_APP_ID` supplied through your credential manager. Hosted Java applications can use `VehicleBrandDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_vehicle_brands.rs) registers `RustWapiVehicleBrandDirectory`. From `rust/`, run `cargo run --example list_vehicle_brands` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `wapi` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Start the Flow with an empty JSON object.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/wapi/VehicleBrandDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/wapi-java
dexcli visualize rust/examples/list_vehicle_brands.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/wapi-rust
```
