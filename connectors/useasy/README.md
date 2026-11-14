# Useasy connector

[Useasy](https://useasy.com/about) is operated by 极简互联（北京）信息科技有限公司. Its [OpenAPI authentication guide](https://help.useasy.cn/developer/authentication/authentication_instructions) specifies HMAC-SHA256 signatures over `appid + timestamp + nonce`, sent with `appid`, six-digit `nonce`, Unix-seconds `timestamp`, and Base64 `signature` headers. The [agent information API](https://help.useasy.cn/developer/seat_management/seat_information) is a read-only `POST /openapi/platform/v1/agent/list` with page metadata. The documented 10 requests/second limit applies. This connector returns only agent ID, work number, enabled state, and page counts; Useasy's password, phone, and email fields never enter the Flow result.

## Connection

Sign in through [Useasy](https://app.useasy.cn/login), open upper-right personal settings, and find `appid`; the account opening form's **鉴权信息** also contains `appid` and `secretKey`. Enter both in the secret `app_id` and `secret_key` fields. Ask your Useasy account manager for the form or API permission if they are unavailable. Leave `baseUrl` blank for the documented production HTTPS endpoint. The connector rejects non-HTTPS and other hosts. Leave timeout and response-size fields blank for their displayed defaults.

`listAgents` accepts one-based `page` and `pageSize`; zero values use page 1 and 20 agents. Page size is capped at 100. Optional `providerRejected`, `invalidResponse`, and `defect` branches remain unwired in the example. HTTP 429, 408, 5xx, and transport failures retry. Useasy response codes other than `200` select `providerRejected`. The public docs do not define a stable error-code taxonomy, so those codes receive a generic provider rejection.

## Example

The [agent directory Flow](go/examples/agent-directory/) is a runnable Worker with static Dex connection name `useasy`. Its mock contains synthetic agent metadata and is verified with a real Dex Worker and Client. Live Useasy authorization, provider pagination, and provider error-code behavior remain unverified without an approved account.
