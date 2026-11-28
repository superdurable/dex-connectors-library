# Tianxia Gongchang connector

Tianxia Gongchang is operated by [北京不等式科技有限公司](https://in-equal.com/天下工厂用户协议). The official [free account endpoint and response](https://www.tianxiagongchang.com/open/docs/capabilities#免费的账户--元数据接口), [quick start](https://www.tianxiagongchang.com/open/docs/quickstart), and [error code contract](https://www.tianxiagongchang.com/open/docs/errors) document this Query. It calls `GET https://open.tianxiagongchang.com/open/v1/account`; the provider says this endpoint does not charge credits or consume its rate limit. It returns application ID, account-wide available credit balance, frozen credits, and sandbox status. It requests no factory or contact data.

## Connection

Follow the [Open Platform quick start](https://www.tianxiagongchang.com/open/docs/quickstart), contact the provider to activate an application, and copy the key from the activation email into Dex Web Connections as secret `api_key`. A live key looks like `sk-tx-live-...`. The connector adds the `Bearer ` prefix. Keep the key secret; ask provider support to revoke it if disclosed. Leave timeout and response-size limit blank for the displayed defaults. The account balance is shared across all applications under the account.

`getAccountStatus` takes no input. It selects `retrieved` for a complete free account response, `providerRejected` for definite authorization or account denial, `invalidResponse` for malformed, oversized, or unexpectedly charged account data, and retries documented 42900, 50000, and 50400 failures plus transport errors. Provider error messages and request IDs are not placed in Flow results.

## Example

The [account status Flow](go/examples/account-status/) is a runnable Worker with static Dex connection name `tianxiagongchang`. Its mock uses synthetic application and credit values and is verified against a real Dex Worker and Client. Live key authorization and account status remain unverified until a user configures a provider connection.

## Python

The [Python package](python/) exposes the same Query and branch contract as Go. Its [runnable example](python/examples/query/) uses static connection name `tianxiagongchang`. The package replays Go-recorded provider scenarios and the example verifies retry and persistence with a real Dex Worker and Client. Live provider authorization has not been verified.

## TypeScript

`@superdurable/connector-tianxiagongchang` provides the `getAccountStatus` Query and a [runnable TypeScript Dex example](typescript/examples/basic/README.md). The package replays Go-recorded provider scenarios with `npm run check`, and `npm run test:integration` verifies retry and persistence against `dexcli dev -open=false`. Real provider credentials and live API behavior have not been verified.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/tianxiagongchang/AccountStatusWorker.java) and [Rust Worker](rust/examples/get_account_status.rs) run `getAccountStatus` with the `tianxiagongchang` connection. Supply `TIANXIAGONGCHANG_API_KEY` through the Worker environment. With Dex running, use `./gradlew accountStatusWorker` from `java/` or `cargo run --example get_account_status` from `rust/`.
