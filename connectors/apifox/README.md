# Apifox connector

`GetProjectAPISummary` reads an accessible project's [OpenAPI 3.1 export](https://apifox-openapi.apifox.cn/api-173411997) and returns only the title, version, and count of HTTP endpoints. The exported definition is discarded, including schemas, descriptions, and examples. This is a read-only POST request to Apifox's export API.

Sign in at [Apifox](https://app.apifox.com), then open avatar > 账号设置 > API 访问令牌 > 新建. Set a name and expiry, save, and copy the one-time secret. The [official token guide](https://docs.apifox.com/api-access-token) explains the steps. In the target project, open 项目设置 > 基本设置 and copy the positive decimal 项目 ID. Configure the token in Dex Web Connections and start the [runnable Flow](go/examples/get-project-api-summary/) with `{"projectID":"123"}` using your real project ID. Leave timeout and maximum response size blank for the displayed defaults.

The connector fixes the documented API version to `2024-03-28`. No live account authorization or export response was verified; provider tests use a fake transport and the example's default mock runs through a real Dex server.
