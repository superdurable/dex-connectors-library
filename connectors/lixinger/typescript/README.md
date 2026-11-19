# Lixinger TypeScript connector

`@superdurable/connector-lixinger` provides the read-only `listStockCompanies` Dex Step for one to one hundred A-share stock codes. It returns basic listing identifiers in provider order and omits prices and financial metrics.

The [runnable Flow and Worker](examples/stock-companies/) use the static `lixinger` connection. At [Lixinger OpenAPI > 我的Token](https://www.lixinger.com/open/api/token), create or locate your token and enter it in Dex Web Connections. Supply unique six-digit stock codes without exchange prefixes. The endpoint, timeout, and response limit have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client with a local provider that returns 503 before succeeding. Real Lixinger authorization and company lookup remain unverified without a user-controlled token.
