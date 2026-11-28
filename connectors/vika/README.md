# Vika

The Go connector lists workspaces available to the configured Vika account. `ListSpaces` returns each workspace's ID, name, and administrator flag. It does not read datasheet records. The operation has no input fields.

Create an API Token in [Vika](https://vika.cn) under the lower-left profile menu → User Center → Developer Configuration → `+`. Vika may first require an email address. Enter the token in the secret Dex Web connection field without `Bearer`. It inherits the account's workspace permissions. See the [official quick start](https://developers.vika.cn/api/quick-start/) and [API reference](https://developers.vika.cn/api/reference/).

The connector calls `GET https://vika.cn/fusion/v1/spaces` over HTTPS. The free plan documents two requests per second per user and datasheet; higher tiers permit more. Rate limits and transient server failures are retried by Dex. No live account authorization has been verified.

The [runnable Dex Flow](go/examples/space-list/README.md) shows connection setup, local mock verification, and Flow output. The logo is the official Vika SVG from the [APITable source repository](https://github.com/apitable/apitable/blob/develop/packages/datasheet/public/logo_text_dark.svg).

Python v0.1.0: [package and runnable example](python/README.md).

## TypeScript

`@superdurable/connector-vika` provides `listSpaces` and a runnable Dex Flow in `typescript/examples/basic/`. Its conformance suite replays the Go-recorded provider scenarios.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/vika/SpaceDirectoryWorker.java) registers `JavaVikaSpaceDirectory`. Run `./gradlew spaceDirectoryWorker` from `java/` with `VIKA_API_TOKEN` supplied through your credential manager. `VIKA_BASE_URL` can override the default API endpoint. Hosted Java applications can use `SpaceDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_spaces.rs) registers `RustVikaSpaceDirectory`. From `rust/`, run `cargo run --example list_spaces` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `vika` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Start the Flow with an empty JSON object.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/vika/SpaceDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/vika-java
dexcli visualize rust/examples/list_spaces.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/vika-rust
```
