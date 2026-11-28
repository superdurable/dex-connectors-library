# Unisound connector

This connector lists predefined text-to-speech voices from [Unisound MaaS](https://maas.unisound.com/docs/api/voice/get-voice). `ListSystemVoices` sends `POST https://maas-api.unisound.com/v1/audio/voices/query` with `voice_type=system` and returns up to 25 voice IDs, names, and language codes. It excludes cloned and designed voices and does not synthesize audio. Unisound's [platform](https://maas.unisound.com/) is operated by the Chinese company 云知声.

## Connection

Sign in at [Unisound MaaS](https://maas.unisound.com/docs/guide/quickstart), complete developer verification, then open 控制台 > 按量计费 > API Key 管理 > 创建 API Key. Copy the secret API Key into Dex Web Connections. The U Plan Key is distinct from the on-demand Key; use one authorized for voice catalog access. Leave timeout and maximum response size blank to use the displayed defaults.

Run the [example Flow](go/examples/list-system-voices/) with `{}` input. The checked-in mock returns one system voice. Live Unisound authorization and catalog behavior require a user-provided Key and have not been verified here.

The Python package includes a [runnable example](python/README.md).

## TypeScript

`@superdurable/connector-unisound` provides `listSystemVoices` and a runnable Dex Flow in `typescript/examples/basic/`. Its conformance suite replays the Go-recorded provider scenarios.

The query returns at most the first 25 system voices. A missing or null display name becomes an empty string, as does a null entry in a voice’s language list.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/unisound/VoiceDirectoryWorker.java) registers `JavaUnisoundVoiceDirectory`. Run `./gradlew voiceDirectoryWorker` from `java/` with `UNISOUND_API_KEY` supplied through your credential manager. Hosted Java applications can use `VoiceDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_system_voices.rs) registers `RustUnisoundVoiceDirectory`. From `rust/`, run `cargo run --example list_system_voices` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `unisound` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Start the Flow with an empty JSON object.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/unisound/VoiceDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/unisound-java
dexcli visualize rust/examples/list_system_voices.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/unisound-rust
```
