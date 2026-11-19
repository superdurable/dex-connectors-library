# Tianyancha company profile

This connector reads selected business registration facts through the [Tianyancha Open Platform interface 365](https://open.tianyancha.com/open/365). Its fixed HTTPS GET request sends a company keyword and a secret token in the `Authorization` header exactly as the official example specifies. The API returns more data than this connector exposes; staff, legal-person, contact, and address fields are discarded. Tianyancha lists this interface at ¥0.25 per call, so the retry policy permits at most two transient retries.

Apply for interface 365 at [Tianyancha Open Platform](https://open.tianyancha.com/open/365). The example code directs users to 数据中心 > 我的接口 to copy the token. Add a `tianyancha` connection in Dex Web with that token; do not prefix it with `Bearer`. Leave timeout and maxResponseBytes blank to use their displayed defaults. Enter a company name, Tianyancha ID, registration number, or unified social credit code in `keyword`.

`GetCompanyProfile` returns only company ID, name, registration status, credit code, industry, registered capital, and company type. `noData` represents official `error_code=300000`. Provider account, permission, balance, and quota errors select `providerRejected`; frequency and temporary service errors retry within the bounded budget. The token and raw provider message never enter Flow output or failures. Redirects are disabled.

Run the [company profile Flow](go/examples/get-company-profile) against `dexcli dev` and connect through Dex Web. The runnable mock demonstrates the full typed Step path without a live token. A live authenticated Tianyancha request and account-specific permission have not been verified.

## Python

The [Python package](python/) exposes the same Query and branch contract as Go. Its [runnable example](python/examples/query/) uses static connection name `tianyancha`. The package replays Go-recorded provider scenarios and the example verifies retry and persistence with a real Dex Worker and Client. Live provider authorization has not been verified.

## TypeScript

`@superdurable/connector-tianyancha` provides the `getCompanyProfile` Query and a [runnable TypeScript Dex example](typescript/examples/basic/README.md). The package replays Go-recorded provider scenarios with `npm run check`, and `npm run test:integration` verifies retry and persistence against `dexcli dev -open=false`. Real provider credentials and live API behavior have not been verified.
