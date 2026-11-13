# Rainbond connector

Rainbond is operated by [北京好雨科技有限公司](https://www.goodrain.com/service/). Its [v5.12 OpenAPI guide](https://v5.12-docs.rainbond.com/docs/intro/) and [team-list contract](https://v5.12-docs.rainbond.com/docs/api/team/getteamlist/) document `GET /openapi/v1/teams`, raw access-token authorization, and the team response. This connector reads one bounded page and outputs team ID, name, active flag, and total count. It excludes creator names, roles, and other personal fields.

## Connection

Open your organization's Rainbond console over HTTPS. In the top-right menu choose **个人中心 > 访问令牌 > 新增**, create a token, and enter it in Dex as `api_key`. Use an account whose team access is limited to what the Flow needs. Enter only the console HTTPS origin as `baseUrl`; the connector adds `/openapi/v1/teams`. The connector rejects non-HTTPS public origins. Timeout and response limit have defaults in Dex Web. The connector targets the documented v5.12 OpenAPI, so check compatibility with other Rainbond versions before using them.

`listTeams` accepts one-based `page` and `pageSize` fields. Zero values use page 1 and 20 items. The connector caps page size at 100, selects `listed` for a valid team page, and safely retries transient failures.

## Example

The [team catalog Flow](go/examples/team-catalog/) is a runnable Worker with static Dex connection name `rainbond`. Its local mock uses synthetic team data and can be verified through a real Dex Worker and Client. Live console authorization, version compatibility, and provider pagination remain unverified until a user connects an approved Rainbond deployment.
