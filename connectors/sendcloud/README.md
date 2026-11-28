# SendCloud (China) account balance

This connector reads [SendCloud user information](https://www.sendcloud.net/doc/email_v2/user_info/) for the configured API_USER. The operation returns only the provider's available balance, total balance, daily request quota, and today's used quota. The provider also returns email, phone, username, and verification details; the connector discards those fields. Decimal balances are preserved as strings, and no currency is inferred because the API reference does not specify one.

SendCloud's [API rules](https://www.sendcloud.net/doc/email_v2/) define `result` and `statusCode`; code `50000` is a rate limit. Both API_USER and API_KEY are Dex credentials sent over HTTPS in a form POST body, which the [userinfo API](https://www.sendcloud.net/doc/email_v2/user_info/) documents, so they never appear in the URL. Errors and Flow outputs omit them. The [runnable Dex Flow](go/examples/account-balance/README.md) demonstrates the static connection and mock-backed verification.

This is the Chinese email platform operated by [武汉闪达科技有限公司](https://www.sendcloud.net/aboutUs/), distinct from the European shipping service with a similar name. The logo comes from SendCloud's official site.

The [TypeScript package](typescript/README.md) implements the same `getAccountBalance` Query with Go-recorded provider conformance and a real Dex Worker/Client example. Live SendCloud authorization requires user-controlled credentials and remains unverified here.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/sendcloud/AccountBalanceWorker.java) and [Rust Worker](rust/examples/get_account_balance.rs) read balances and daily quotas with the `sendcloud` connection. Supply `SENDCLOUD_API_USER` and `SENDCLOUD_API_KEY` through the Worker environment. With Dex running, use `./gradlew accountBalanceWorker` from `java/` or `cargo run --example get_account_balance` from `rust/`.
