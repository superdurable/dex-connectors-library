# Guance connector

This connector reads one page of [Guance dashboard metadata](https://docs.guance.com/en/open-api/board/list/) through `GET https://openapi.guance.com/api/v1/dashboards/list`. It returns only dashboard UUIDs and names plus pagination. The provider also returns chart layout and user information; the connector excludes both from durable output.

Create a secret API Key in the target workspace at **管理 > API Key 管理 > 新建 Key**, selecting a role with dashboard read permission. The [official API Key guide](https://docs.guance.com/management/api-key/) says only an administrator or Owner can create a key, and the key determines its workspace. Copy the Key (密钥), not Key ID, into Dex Web Connections. The connector sends it in `DF-API-KEY` and never places it in a URL or Flow result. The fixed endpoint is Guance's Alibaba Cloud SaaS node; other nodes require a future configuration design.

The [runnable Flow](go/examples/list-dashboards-page/README.md) uses the static `guance` connection. Real API Key authorization and Dex Web form interaction remain unverified.
