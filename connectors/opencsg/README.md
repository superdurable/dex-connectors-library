# OpenCSG AI Gateway connector

`ListGatewayModels` reads the model IDs available to an OpenCSG AI Gateway API Key. It calls the official [`GET /v1/models` endpoint](https://opencsg.com/docs/csghub/101/function/aigateway/model_access) over HTTPS with a Bearer Key. The response includes the total number of returned models and at most the first 50 IDs. Inference, model descriptions, and account data are excluded.

## Authorization

Sign in to [OpenCSG](https://hub.opencsg.com), then open account settings > API Keys for a personal Key or organization settings > API Keys for a team Key. Select 新建 API Key, enter a name, optionally set quota and expiry, and copy the `gw-sk-` secret shown at creation. Paste it into Dex Web Connections. This is an AI Gateway API Key, separate from a Git Access Token. The [official API Keys guide](https://opencsg.com/docs/profile/api_keys) describes these paths. Leave the request timeout and maximum response size blank to use the manifest defaults.

## Example

The [runnable Flow](go/examples/list-gateway-models/) starts with `{}` and completes with a typed `ListGatewayModelsOutput`. Its default mock returns one model. Run the same Flow with a user-controlled Dex Web Connection for live authorization. Live OpenCSG authorization, model visibility, and provider response variation have not been verified in this change.

## Python

The first Python package implements the read-only operation and all Go-recorded provider branches. See [Python package](python/README.md) and its [runnable Flow](python/examples/gateway_models/README.md). Live provider authorization and responses are unverified.

## TypeScript, Java, and Rust

The [TypeScript Flow](typescript/examples/basic/flow.ts), [Java Worker](java/examples/io/superdurable/dexconnectors/opencsg/GatewayCatalogWorker.java), and [Rust Worker](rust/examples/list_gateway_models.rs) each bind the `opencsg` connection and accept an empty object. All five languages return the full count and preserve the order of the first fifty IDs. An invalid ID anywhere in the response, including an ID containing the current bearer key, selects `invalidResponse`.

Run `./gradlew gatewayCatalogWorker` from `java/` with `OPENCSG_API_KEY` supplied by your credential manager. Java applications using a loaded Dex project can bind `GatewayCatalog.fromProject` instead. The Rust example loads its credentials and request limits from the project. Point `DEX_CONNECTOR_CONNECTIONS_FILE` at the Dex Web connections file and run `cargo run --example list_gateway_models` from `rust/`.

With `DEX_FLOW_SERVICE_ADDRESS` set to a running Dex service, `./gradlew test dexDevTest` and `cargo test --all-targets` run the native samples against provider fixtures. From this connector directory, generate and lint their FDG 2.0 graphs with the following commands.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/opencsg/GatewayCatalogWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/opencsg-java-fdg
dexcli visualize rust/examples/list_gateway_models.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/opencsg-rust-fdg
```
