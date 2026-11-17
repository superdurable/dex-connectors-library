# Cloopen account balance

The Go connector performs the official read-only [AccountInfo lookup](https://doc.yuntongxun.com/p/5a533b4b3b8496dd00dce034). It returns only the main-account balance as Cloopen's exact decimal string, without guessing its currency or units. The response's account name, status, and dates are dropped.

The [REST authentication contract](https://doc.yuntongxun.com/p/5a533ac73b8496dd00dce025) requires HTTPS, a UTC timestamp, uppercase MD5 signature of `ACCOUNT SID + AUTH TOKEN + timestamp`, and Base64 authorization of `ACCOUNT SID + : + timestamp`. The token never enters Flow input or output. Main-account credentials appear in the Cloopen management console. The docs do not specify a charge for this read-only lookup.

`getBalance` selects `fetched`, `providerRejected`, `invalidResponse`, or `defect`. HTTP 429, temporary 5xx, transport failures, and documented concurrency or internal errors retry. Optional branches fail the example Flow unless wired by an application.

[Run the balance Flow](go/examples/balance/README.md). Its mock is synthetic; live balance lookup has not been verified without user-controlled credentials in Dex Web Connections.

## Python package

The [Python v0.1.0 package](python/README.md) implements `getBalance` with the
same Go-recorded signed requests and outcomes. Its
[runnable Flow](python/examples/balance/README.md) reads through a real Dex
Worker and Client and persists only the decimal balance string. The local
integration test uses inert credentials and a fake provider; live Cloopen
authorization remains unverified.

## TypeScript package

The `@superdurable/connector-cloopen` package implements `getBalance` with the same signature, request, and outcomes as Go. Its [runnable balance Flow](typescript/examples/balance/README.md) passed a real local Dex Worker, Client, retry, and persistence test against a provider stand-in. Fourteen Go-recorded scenarios verify exact signed requests and outcomes. Live Cloopen account access remains unverified.
