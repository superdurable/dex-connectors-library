# Rainbond TypeScript connector

`@superdurable/connector-rainbond` implements the read-only `listTeams` Step. It requests one page from your Rainbond console’s `GET /openapi/v1/teams` endpoint and returns team IDs, names, active flags, and total count. It excludes owner IDs, creator names, roles, and timestamps.

The [runnable Flow and Worker](examples/team-list/) use the static `rainbond` connection. In your organization’s HTTPS Rainbond console, open the top-right user menu > 个人中心 > 访问令牌 > 新增 and copy the secret access token into Dex Web Connections. Copy only the console HTTPS origin into `baseUrl`, without `/openapi`; blank prevents requests. See the [Rainbond v5.12 documentation](https://v5.12-docs.rainbond.com/docs/intro/). The operation input chooses a page and page size; zeros mean page 1 and 20. The token stays in Credentials and is resolved per request.

The Go-recorded conformance suite checks exact requests and all outcomes. The integration test runs a real Dex Worker and Client against a local provider that returns 503 once before success. Real console authorization and team visibility remain unverified.
