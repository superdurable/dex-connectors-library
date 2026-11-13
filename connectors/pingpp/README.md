# Ping++ Charge status

The Go connector performs the official read-only [Charge lookup](https://docs.pingxx.com/api/%E6%9F%A5%E8%AF%A2%20Charge%20%E5%AF%B9%E8%B1%A1.html). It returns the Charge ID and four state flags: `paid`, `refunded`, `reversed`, and `liveMode`. It never returns customer IP, order number, payment credential, metadata, channel details, or transaction number. It never creates or changes a payment.

Ping++ [API authentication](https://www.pingxx.com/api/%E8%AE%A4%E8%AF%81.html) uses HTTPS Basic Auth with the API Key as username and an empty password. Test (`sk_test`) and live (`sk_live`) keys access separate data. The official [error reference](https://docs.pingxx.com/api/%E9%94%99%E8%AF%AF.html) documents 401 for bad keys, 403 for concurrency limits, 404 for absent resources, and 5xx for server faults.

`getChargeStatus` selects `fetched`, `notFound`, `providerRejected`, `invalidResponse`, or `defect`. Concurrency limits, rate limits, transport errors, and temporary server faults retry. Optional branches fail the example Flow unless wired by an application.

[Run the Charge status Flow](go/examples/charge-status/README.md). Its checked-in mock is synthetic; live payment state has not been verified without a user-controlled API Key in Dex Web Connections.
