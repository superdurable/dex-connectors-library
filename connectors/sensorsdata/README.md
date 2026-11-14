# Sensors Data connector

Sensors Data (神策数据) is operated by [神策网络科技（北京）有限公司](https://www.sensorsdata.cn/compliance/privacy.html). Its official [Analytics channel campaign API](https://manual.sensorsdata.cn/sa/docs/channel_doc/v0300), [OpenAPI overview](https://manual.sensorsdata.cn/openapi), and [API Key authorization guide](https://manual.sensorsdata.cn/sa/docs/open_api_authentication) document this Query. The connector reads one page with `POST /api/v3/analytics/v1/channel/campaigns/list` and returns campaign name, channel link count, last creation time, and pagination totals. No user events, identities, or customer records are requested.

## Connection

Sign in to your organization's Sensors Data Analytics site over HTTPS. Copy the site origin to `baseUrl` and the exact English `project` parameter from the browser URL to `projectName`. Open **个人账号 > API Key 管理 > 新增 Api Key** and copy an unexpired 35-character API Key to secret `api_key` in Dex Web Connections. The key inherits the user's project permissions; use a read-only account and revoke a disclosed key from the same page. Leave timeout and response-size limit blank for the displayed defaults. The connector rejects non-HTTPS origins and sends `api-key` and `sensorsdata-project` headers.

`listCampaigns` accepts zero-based `pageNum` and `pageSize` up to 1000. Zero size uses 20. It selects `listed` for a valid page, `providerRejected` for definite denial, `invalidResponse` for malformed or oversized data, and retries 429, transient server errors, and transport failures. Sensors Data recommends low-frequency API queries and at most ten concurrent requests; this example runs one page per Flow.

## Example

The [campaign catalog Flow](go/examples/campaign-catalog/) is a runnable Worker with static Dex connection name `sensorsdata`. Its mock uses synthetic campaign metadata and is verified against a real Dex Worker and Client. Live cluster authorization, product-version compatibility, and campaign data remain unverified until a user connects an approved HTTPS deployment.
