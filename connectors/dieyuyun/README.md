# Wuliang AI (无量AI) connector

The operator is [Dieyuyun Technology (Shenzhen) Co., Ltd.](https://open.dieyuyun.com/legal/terms.html). This connector calls the official [`GET https://open.dieyuyun.com/v1/balance`](https://open.dieyuyun.com/guide/api-reference.html) endpoint. The Query returns only available tenant balance in CNY yuan, preserving the exact JSON number text. It filters API Key ID, name, daily and monthly budgets, and other account details.

## Connection

Sign in to the [Wuliang AI console](https://open-console.dieyuyun.com/), open **API Keys > Key Management**, click **Create API Key**, select an application, and copy the one-time-shown secret into the Dex `api_key` credential. The [official key guide](https://open.dieyuyun.com/guide/manage-api-key.html) describes permission and budget controls. Dex sends the key only in the Authorization Bearer header. Leave base URL, timeout, and response limit blank for their displayed defaults. Disable or delete a compromised key in Key Management and create a replacement.

## Go

The [account balance Flow](go/examples/account-balance/) is a runnable Worker with static Dex Web connection name `dieyuyun`. `fetched` completes the Flow; optional branches fail it unless an application wires a business response. Read-only transient failures retry safely. The local mock integration test uses synthetic balance data and a real Dex Worker and Client. Live authorization and balance values remain unverified without a user-controlled connection.

## Python

The [Python account balance Flow](python/examples/account_balance/) uses the same static connection and checks exact decimal output across a real Dex Worker and Client retry. Its package replays the Go-recorded provider scenarios. Live authorization and actual tenant balance remain unverified without a user-controlled connection.

## TypeScript

The [TypeScript balance Flow](typescript/examples/balance/) is a runnable Worker and Client using the same static `dieyuyun` connection. It preserves the provider decimal balance text without floating-point conversion and persists only `availableCNY`.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/dieyuyun/AccountBalanceWorker.java) and [Rust Worker](rust/examples/get_account_balance.rs) execute `getAccountBalance` through the static `dieyuyun` connection. The result retains the exact decimal representation, including trailing zeroes and exponent notation. Set `DEX_FLOW_SERVICE_ADDRESS` and `DIEYUYUN_API_KEY` in the Worker process environment.

Run `./gradlew accountBalanceWorker` from `java/` or `cargo run --example get_account_balance` from `rust/`. With a local Dex server running, use `./gradlew build dexDevTest` and `cargo test --all-targets` to verify provider outcomes and the Worker retry path. Generate the Flow graphs from the connector directory:

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/dieyuyun/AccountBalanceWorker.java --schema-version 2.0 --lint app --open=false
dexcli visualize rust/examples/get_account_balance.rs --schema-version 2.0 --lint app --open=false
```
