# Shumei

The Go connector reads the current Shumei SaaS account balance in Chinese yuan. `GetBalance` has no caller input and returns only `balanceYuan`; it does not read bills, risk events, or customer data.

Shumei's [official open API manual](https://help.ishumei.com/docs/correction/openness/) documents `POST https://api-web-bj.fengkongcloud.com/saas/balance/v1` with `accessKey` in a JSON request body. It returns `code: 1100` and `content.balance` on success. The key is provided by Shumei when account service is activated; begin at the [Shumei console](https://console.ishumei.com/) and contact your account manager if it is not available. Enter it in Dex Web's secret `access_key` field. The key is never included in a URL, Flow output, or error message.

The [runnable Dex Flow](go/examples/balance-check/README.md) shows connection setup and output. The company logo embeds the unmodified [official website logo image](https://www.ishumei.com/next/img/global/logo3.png). No live account authorization has been verified.

## Python

The `python/` package provides the `getBalance` Query. Run its checked-in example from `python/examples/`.
## TypeScript

`@superdurable/connector-shumei` provides the `getBalance` Query and a [runnable TypeScript Dex example](typescript/examples/basic/README.md). The package replays Go-recorded provider scenarios with `npm run check`, and `npm run test:integration` verifies retry and persistence against `dexcli dev -open=false`. Real provider credentials and live API behavior have not been verified.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/shumei/AccountBalanceWorker.java) and [Rust Worker](rust/examples/get_balance.rs) execute `getBalance` through the static `shumei` connection. The result contains the available balance in yuan. Set `DEX_FLOW_SERVICE_ADDRESS` and `SHUMEI_ACCESS_KEY` in the Worker process environment.

Run `./gradlew accountBalanceWorker` from `java/` or `cargo run --example get_balance` from `rust/`. With a local Dex server running, use `./gradlew build dexDevTest` and `cargo test --all-targets` to verify provider outcomes and the Worker retry path. Generate the Flow graphs from the connector directory:

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/shumei/AccountBalanceWorker.java --schema-version 2.0 --lint app --open=false
dexcli visualize rust/examples/get_balance.rs --schema-version 2.0 --lint app --open=false
```
