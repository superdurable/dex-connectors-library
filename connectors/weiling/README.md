# Weiling connector

Weiling (卫瓴科技), operated by 上海卫瓴信息科技有限公司, provides collaborative CRM. This connector reads department IDs, names, and parent IDs. It excludes leader and employee identifiers, contacts, and tokens.

The [official OpenAPI guide](https://s.apifox.cn/19699d3b-8e70-434a-b516-ce35107d2094/doc-936771) documents HTTPS, the administrator credential path, response codes, and frequency limits. The [token endpoint](https://s.apifox.cn/19699d3b-8e70-434a-b516-ce35107d2094/api-25106283) returns an access token and expiry. The [department list](https://s.apifox.cn/19699d3b-8e70-434a-b516-ce35107d2094/api-19870063) returns the full accessible directory. Token responses are cached in the connector process until two minutes before expiry; credential rotation invalidates the cache. No token is stored in Flow state.

## Connection

As a Weiling CRM administrator, open **设置 > 开放平台** and copy AppId and AppSecret into the secret connection fields. Leave endpoint and limits blank for defaults. The token endpoint uses provider-defined GET query parameters, so the connector never logs request URLs or credentials. Provider 429, 408, 5xx, transport failures, and documented rate code 1009 retry; other rejections take an optional branch.

Run the [department directory example](go/examples/department-directory/README.md). Its synthetic mock is not live Weiling data. Live credentials, permissions, and actual department response compatibility remain unverified.
