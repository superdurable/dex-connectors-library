# Fanyigou connector

`GetAccountBalance` reads the document-translation resource balance and its billing unit. The official `deductType` is 0 for points or 1 for pages; `balance` uses that unit. The connector selects resource package type 1 and returns no account identity or translation content.

At [Fanyigou Open Platform](https://open.fanyigou.com/), complete developer registration and create an application. Copy its appid from Console > Applications and privatekey from profile avatar > Overview > Developer Information > Key into a Dex Web Connection named `fanyigou`. Both are secret fields. The connector uses the documented Bearer privatekey method and generates a random nonce for each form request.

The official [account API and authentication guide](https://open.fanyigou.com/filecore.html) defines the endpoint, response, and provider codes. The checked-in [Go Flow](go/examples/get-account-balance/) and [Python Flow](python/examples/get_account_balance/) use real Dex Workers with mock provider responses. Live Fanyigou authorization and balance remain unverified.

The [TypeScript package](typescript/) exposes `getAccountBalance` and includes a runnable real Dex Worker/Client example and replay of all Go-recorded provider scenarios.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/fanyigou/AccountBalanceWorker.java) and [Rust Worker](rust/examples/get_account_balance.rs) execute `getAccountBalance` through the static `fanyigou` connection. The result contains the remaining translation balance and its unit (points or pages). Set `DEX_FLOW_SERVICE_ADDRESS` and the Fanyigou connection variables `FANYIGOU_APP_ID` and `FANYIGOU_PRIVATE_KEY` in the Worker process environment.

Run `./gradlew accountBalanceWorker` from `java/` or `cargo run --example get_account_balance` from `rust/`. With a local Dex server running, use `./gradlew build dexDevTest` and `cargo test --all-targets` to verify provider outcomes and the Worker retry path. Generate the Flow graphs from the connector directory:

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/fanyigou/AccountBalanceWorker.java --schema-version 2.0 --lint app --open=false
dexcli visualize rust/examples/get_account_balance.rs --schema-version 2.0 --lint app --open=false
```
