# Likelic Room Directory

`ListRooms` reads one page of group identifiers and notification readiness from the [Likelic Agent API](https://agent.likelic.com/doc/v1/). The [official service terms](https://www.likelic.com/terms/) identify 苏州立刻电子商务有限公司 as the Chinese operator. The connector returns only each room's channel, robot instance ID, room ID, and push state plus the page and total count. Group names, company names, member counts, and the raw provider response are discarded.

Sign in at [Likelic Agent](https://agent.likelic.com), obtain your Customer Code from the enterprise administrator, create a secret `sk-` API Key at 外部拓展 > API 设置, then enable 推送通知 and authorize the Key under 系统设置 > 开放 API 权限. Add the code and Key to a Dex Web `likelic` connection. Pass a one-based page number; each request reads 20 rooms. Blank timeout and maxResponseBytes use the displayed defaults.

The [runnable Dex example](go/examples/list-rooms/) uses a synthetic mock. Live tenant authorization, room visibility, and pagination remain unverified without user-controlled provider credentials.

The [Python package](python/) implements the same `ListRooms` Query with Go-recorded replay and a real Dex Worker/Client mock example. Live provider authorization remains unverified.

The [TypeScript package](typescript/README.md) implements the same `listRooms` Query, including Go-recorded conformance replay and a real Dex Worker and Client integration test.

An omitted total count is returned as zero. Room identifiers are limited to 256 UTF-8 bytes.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/likelic/RoomDirectoryWorker.java) registers `JavaLikelicRoomDirectory`. Run `./gradlew vehicleBrandDirectoryWorker` from `java/` with `LIKELIC_API_KEY` and the customer code in `LIKELIC_CUSTOMER_CODE` supplied through your credential manager. Hosted Java applications can use `RoomDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_rooms.rs) registers `RustLikelicRoomDirectory`. From `rust/`, run `cargo run --example list_rooms` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `likelic` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Start the Flow with `{"page":1}` for the first page.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/likelic/RoomDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/likelic-java
dexcli visualize rust/examples/list_rooms.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/likelic-rust
```
