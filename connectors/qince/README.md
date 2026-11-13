# Qince connector

Qince (勤策) is operated by [南京掌控网络科技有限公司](https://qince.com/p-faq-1743.html). Its official [OpenAPI signing specification](https://api.qince.com/SERVER/sys/api_introduction.html), [organization query contract](https://api.qince.com/SERVER/sysapp/organization.html), [activation guide](https://api.qince.com/SERVER/sys/introduction.html), and [region discovery guide](https://api.qince.com/SERVER/sys/help.html) document this Query. The connector sends `POST /api/organization/v1/queryOrganization/{openid}/{timestamp}/{digest}/{msg_id}` with body `{}` and the documented MD5 signature over `body|appkey|timestamp`. It returns department ID, code, name, and active status only; employee and customer records are not requested.

## Connection

The enterprise administrator enables OpenAPI under **系统管理 > 集成管理 > API管理**, or asks the account manager for the 19-character OpenID and 18-character secret AppKey. Enter them as `open_id` and `app_key` in Dex Web Connections. In Qince WEB, search **API授权**, scan its QR code with the administrator's mobile app, and copy the displayed HTTPS OpenAPI origin to `baseUrl`. It may include a region, such as `https://openapi.region2.qince.com`. The connector accepts only official `openapi.*.qince.com` HTTPS origins. Leave timeout and response-size limit blank for the displayed defaults.

`listDepartments` takes no input and requests the organization directory. It selects `listed` for a valid response, `providerRejected` for a definite denial, `invalidResponse` for malformed or oversized data, and retries rate limits, temporary server failures, and transport interruptions. The response is bounded to the configured byte limit. The documented response_data may be a JSON string or an array; both forms are accepted. No personnel details enter Flow results.

## Example

The [department catalog Flow](go/examples/department-catalog/) is a runnable Worker with static Dex connection name `qince`. Its mock uses synthetic department metadata and is verified against a real Dex Worker and Client. Live OpenAPI activation, region selection, and department data remain unverified until an authorized enterprise connection is configured.
