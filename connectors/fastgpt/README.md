# FastGPT connector

[FastGPT](https://doc.fastgpt.cn/zh-CN/guide/version/cloud/terms) is operated in China by 广州环际云计算有限公司. Its current [System OpenAPI knowledge-base reference](https://doc.fastgpt.cn/zh-CN/openapi/dataset) documents `GET /api/core/dataset/detail?id=<id>` and Bearer personal API Key authorization. This connector returns only knowledge-base ID, name, type, and status, excluding descriptions, model configuration, access details, and indexed content.

## Connection

For the China cloud, sign in at [cloud.fastgpt.cn](https://cloud.fastgpt.cn/), open **账号 > API 密钥**, and create a personal API Key. A team owner may need to grant **创建 API 密钥** permission. Use a member with read access to the intended knowledge base. Application Keys from **工作台 > 应用 > 发布渠道 > API 访问** only authorize that application's chat APIs. Enter the personal key as `api_key`; leave `baseUrl` blank for the displayed China cloud origin, or enter your organization's HTTPS private instance address, including any sub-path prefix such as `/fastgpt` but without `/api`. See the [official API key guide](https://doc.fastgpt.cn/zh-CN/openapi/intro).

`getDatasetDetails` takes an `id` copied from the knowledge-base interface URL. The `found` branch contains only metadata. Authorization and protocol failures select optional branches; transient read failures retry.

## Example

The [dataset details Flow](go/examples/dataset-details/) is a runnable Worker with static Dex connection name `fastgpt`. Its local mock uses synthetic data and can be verified through a real Dex Worker and Client. Live key authorization and actual knowledge-base access remain unverified without a user-controlled connection.
