# TextIn connector

This Go connector calls TextIn's [official balance API](https://www.textin.com/document/open_api_balance) at `GET https://api.textin.com/user/finance/balance`. The read-only `getAccountBalance` Query returns only `availableTCoins` and `payType` (1 prepaid, 2 postpaid). It omits total balance, unpaid amount, and other account data. Available T-coins are preserved as the exact JSON number text, avoiding floating-point rounding.

## Connection

Sign in to [TextIn](https://www.textin.com/), open **Workbench > Account Settings > Developer Information**, then copy `x-ti-app-id` into `app_id` and `x-ti-secret-code` into the secret `secret_code` field. The [official account guide](https://www.textin.com/doc/guide/account/%E5%A6%82%E4%BD%95%E8%8E%B7%E5%8F%96app%20id) also shows where to find App ID. Dex sends these only as the corresponding provider request headers. Leave the API base URL, timeout, and response size blank for their displayed defaults. Rotate the secret in the TextIn workbench if compromised.

## Go

The [account balance Flow](go/examples/account-balance/) is a runnable Worker with static Dex Web connection name `textin`. `fetched` completes the Flow. Unwired optional branches fail it. HTTP 408, 429, and 5xx and provider code 500 retry safely because the operation only reads the account. The mock integration test uses synthetic sample data and a real Dex Worker and Client. No live credentials are bundled; live authorization and balance values remain unverified.

The [TypeScript package and runnable Flow](typescript/) replay the Go provider scenarios for `getAccountBalance`.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/textin/AccountBalanceWorker.java) and [Rust Worker](rust/examples/get_account_balance.rs) execute `getAccountBalance` through the static `textin` connection. The result retains the exact decimal representation, including trailing zeroes and exponent notation. Set `DEX_FLOW_SERVICE_ADDRESS` and the TextIn connection variables `TEXTIN_APP_ID` and `TEXTIN_SECRET_CODE` in the Worker process environment.

Run `./gradlew accountBalanceWorker` from `java/` or `cargo run --example get_account_balance` from `rust/`. With a local Dex server running, use `./gradlew build dexDevTest` and `cargo test --all-targets` to verify provider outcomes and the Worker retry path. Generate the Flow graphs from the connector directory:

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/textin/AccountBalanceWorker.java --schema-version 2.0 --lint app --open=false
dexcli visualize rust/examples/get_account_balance.rs --schema-version 2.0 --lint app --open=false
```
