# Hesi connector

[Hesi](https://www.ekuaibao.com/) (合思, formerly 易快报) publishes its [OpenAPI guide](https://ekuaibao.github.io/open-platform-docs/docs/open-api/getting-started), [authorization endpoint](https://ekuaibao.github.io/open-platform-docs/docs/open-api/getting-started/auth), [address-prefix lookup](https://ekuaibao.github.io/open-platform-docs/docs/open-api/getting-started/origin), and [department list](https://ekuaibao.github.io/open-platform-docs/docs/open-api/corporation/get-departments). This connector exchanges the configured AppKey and AppSecurity for a short-lived accessToken on each Query, then reads one department page. It returns only department ID, name, code, active state, total, and next offset. It does not return cost centers, legal entities, employees, or the accessToken.

## Connection

Ask your Hesi implementation consultant to enable **开放接口(新)**. In Hesi desktop, open **合思+ > 开放接口(新)** and create or inspect the AppKey and AppSecurity; enter both in secret fields. Find the corporate ID in **企业信息**, then use the documented address-prefix lookup to determine your organization's HTTPS API origin. Leave `baseUrl` blank if it matches the standalone-platform default; otherwise enter only the returned HTTPS origin. The connector accepts the documented Hesi platform hosts and rejects HTTP. Leave timeout and response-size settings blank for their displayed defaults.

`listDepartments` accepts zero-based `start` and `count`; zero count uses 20 and count is capped at 100. The provider's API requires the short-lived token in a query parameter. This connector never logs or returns it; avoid HTTP debugging that logs full request URLs. Optional `providerRejected`, `invalidResponse`, and `defect` branches remain unwired in the example. HTTP 429, 408, 5xx, and transport failures retry. The public department list documents HTTP 403 for expired authorization and a malformed page case; the connector classifies these separately.

## Example

The [department directory Flow](go/examples/department-directory/) is a runnable Worker with static Dex connection name `hesi`. Its mock is synthetic and verified with a real Dex Worker and Client. Live Hesi authorization, tenant origin selection, and department pagination remain unverified without an approved enterprise account.
