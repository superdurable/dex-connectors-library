# Sensors Data connector for Python

Install `dex-connector-sensorsdata==0.1.0` after its first release. The package implements the `listCampaigns` Query with `new_list_campaigns_step`. It reads one bounded page of channel campaign names, link counts, last use times, and pagination metadata; it does not return user or event records.

Create a Dex Web Connection named `sensorsdata`. In your organization's Sensors Data Analytics HTTPS site, open 个人账号 > API Key 管理 > 新增 Api Key and create a project-scoped read key. Set the site HTTPS origin as `baseUrl` and the browser URL's project English name as `projectName`. Save the 35-character key in Dex's protected `api_key` field. See [Sensors Data OpenAPI authentication](https://manual.sensorsdata.cn/sa/docs/open_api_authentication). Leave timeout and response bound blank for displayed defaults.

The happy branch is `listed`. `providerRejected`, `invalidResponse`, and `defect` are optional and fail the example Flow when selected. HTTP 408, 429, 5xx, and transport failures retry. The [runnable example](examples/campaign_catalog/README.md) includes a Worker and real Dex integration test using a fake provider. Live Sensors Data authorization and responses remain unverified.

## Verification

```bash
uv sync --group dev
uv run pytest tests
uv run ruff check .
uv run mypy src
```
