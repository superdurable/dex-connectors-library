# Qiyuesuo connector

[契约锁 Qiyuesuo](https://www.qiyuesuo.com/) is operated by Shanghai Genyan Network Technology Co., Ltd. (上海亘岩网络科技有限公司), as shown in its [official documentation footer](https://open.qiyuesuo.com/document). The current [API list](https://help.qiyuesuo.com/document/open/detail/2725986623018775399) documents the HTTPS production and sandbox hosts and `GET /v2/contract/list`; the [contract-list contract](https://open.qiyuesuo.com/document/2657159506219249750) defines offset pagination, `responseCode`, `result.totalCount`, and contract `id`/`status`. The [API protocol](https://open.qiyuesuo.com/document/2279605535600803840) defines MD5 signature headers. This connector returns only decimal contract ID strings and statuses, plus total count and next offset. It never emits signer details, names, contract bodies, or credentials.

## Connection

[Register an API application](https://open.qiyuesuo.com/signup) and complete enterprise verification. Ask Qiyuesuo to enable the contract-list API for your application. The [current application migration notice](https://open.qiyuesuo.com/notice/detail?id=2708871885193495439) directs users to the electronic signing platform's **集成管理** page. Select the application to copy its App Token into secret `app_token`, and open **系统配置** to copy its App Secret into secret `app_secret` and configure the IP allowlist. Leave `baseUrl` blank for the production HTTPS host; choose the documented sandbox HTTPS host only when testing. Timeout and response-size fields have safe Dex Web defaults.

`listContracts` accepts zero-based `offset` and `limit`. The default limit is 20, capped at 100. The Query fetches one bounded page. A successful response selects `listed`; optional failure branches are unwired in the example. HTTP 429/408/5xx, documented application rate limiting, internal errors, and transport failures retry safely. Credential and permission failures select `providerRejected`. The signature includes a fresh millisecond timestamp and UUID nonce for every attempt. The operation does not create, sign, or download a contract.

## Example

The [contract status Flow](go/examples/contract-status/) is a runnable Worker with static Dex connection name `qiyuesuo`. Its local mock uses synthetic contract IDs and is verified with a real Dex Worker and Client. Live authorization, permission setup, and provider pagination remain unverified until connected to an approved Qiyuesuo application.
