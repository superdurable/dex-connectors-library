# Onebound Python connector

`dex-connector-onebound` implements the signed `searchProducts` Query. Configure the key and secret from the [Onebound console](https://console.open.onebound.cn/console/) in Dex Web Connections. The Query returns a bounded public 1688 product page. Calls may be billed.

See the [runnable product-search Flow](examples/product_search/). Go-recorded conformance scenarios cover signed requests and decisions. The checked-in Dex integration test uses a fake provider; live Onebound authorization and search remain unverified.
