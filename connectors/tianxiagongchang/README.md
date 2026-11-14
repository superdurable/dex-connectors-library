# Tianxia Gongchang connector

Tianxia Gongchang is operated by [北京不等式科技有限公司](https://in-equal.com/天下工厂用户协议). The official [free account endpoint and response](https://www.tianxiagongchang.com/open/docs/capabilities#免费的账户--元数据接口), [quick start](https://www.tianxiagongchang.com/open/docs/quickstart), and [error code contract](https://www.tianxiagongchang.com/open/docs/errors) document this Query. It calls `GET https://open.tianxiagongchang.com/open/v1/account`; the provider says this endpoint does not charge credits or consume its rate limit. It returns application ID, account-wide available credit balance, frozen credits, and sandbox status. It requests no factory or contact data.

## Connection

Follow the [Open Platform quick start](https://www.tianxiagongchang.com/open/docs/quickstart), contact the provider to activate an application, and copy the key from the activation email into Dex Web Connections as secret `api_key`. A live key looks like `sk-tx-live-...`. The connector adds the `Bearer ` prefix. Keep the key secret; ask provider support to revoke it if disclosed. Leave timeout and response-size limit blank for the displayed defaults. The account balance is shared across all applications under the account.

`getAccountStatus` takes no input. It selects `retrieved` for a complete free account response, `providerRejected` for definite authorization or account denial, `invalidResponse` for malformed, oversized, or unexpectedly charged account data, and retries documented 42900, 50000, and 50400 failures plus transport errors. Provider error messages and request IDs are not placed in Flow results.

## Example

The [account status Flow](go/examples/account-status/) is a runnable Worker with static Dex connection name `tianxiagongchang`. Its mock uses synthetic application and credit values and is verified against a real Dex Worker and Client. Live key authorization and account status remain unverified until a user configures a provider connection.
