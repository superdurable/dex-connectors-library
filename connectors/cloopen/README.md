# Cloopen account balance

The Go connector performs the official read-only [AccountInfo lookup](https://doc.yuntongxun.com/p/5a533b4b3b8496dd00dce034). It returns only the main-account balance as Cloopen's exact decimal string, without guessing its currency or units. The response's account name, status, and dates are dropped.

The [REST authentication contract](https://doc.yuntongxun.com/p/5a533ac73b8496dd00dce025) requires HTTPS, a UTC timestamp, uppercase MD5 signature of `ACCOUNT SID + AUTH TOKEN + timestamp`, and Base64 authorization of `ACCOUNT SID + : + timestamp`. The token never enters Flow input or output. Main-account credentials appear in the Cloopen management console. The docs do not specify a charge for this read-only lookup.

`getBalance` selects `fetched`, `providerRejected`, `invalidResponse`, or `defect`. HTTP 429, temporary 5xx, transport failures, and documented concurrency or internal errors retry. Optional branches fail the example Flow unless wired by an application.

[Run the balance Flow](go/examples/balance/README.md). Its mock is synthetic; live balance lookup has not been verified without user-controlled credentials in Dex Web Connections.

The [TypeScript package](typescript/README.md) implements `getBalance` and includes a [runnable Dex Flow](typescript/examples/balance/README.md). Its provider scenarios replay the Go-recorded contract; live provider authorization remains unverified without user-controlled credentials.

The [Python package](python/README.md) implements `getBalance` and includes a [runnable Dex Flow](python/examples/balance/README.md). Its provider scenarios replay the Go-recorded contract; live provider authorization remains unverified without user-controlled credentials.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/cloopen/AccountBalanceWorker.java) and [Rust Worker](rust/examples/get_balance.rs) run `getBalance` with the `cloopen` connection. Supply `CLOOPEN_ACCOUNT_SID` and `CLOOPEN_AUTH_TOKEN` through the Worker environment. With Dex running, use `./gradlew accountBalanceWorker` from `java/` or `cargo run --example get_balance` from `rust/`.
