# OKKI CRM

The Go `listCustomerFields` Query reads the tenant's customer field dictionary from the [official GET `/v1/company/fields` API](https://open.xiaoman.cn/api-3478270). It returns only field IDs, labels, and type codes. Field defaults, option values, customer records, and contact details are excluded. The result is bounded to 500 fields and 1 MiB of provider JSON by default.

## Connection

Sign in as the intended OKKI CRM company's main account and open **企业管理 → 外部对接 → API对接**. Enable API integration and copy that company's client ID and secret into Dex Web Connections. The [official quick start](https://open.xiaoman.cn/) labels `https://api-sandbox.xiaoman.cn` as the **正式域名**, despite the host name. Its [client credentials documentation](https://open.xiaoman.cn/api-3473041) defines `POST /v1/oauth2/access_token` and the `company` scope. Dex obtains and renews the short-lived token automatically; leave the access token field blank. Create a separate Dex connection for each OKKI CRM company because the platform isolates tenant data.

The connector sends `GET /v1/company/fields?type=company` with the access token in the `Authorization` header, as shown in the [official API example](https://open.xiaoman.cn/api-3478270). HTTP 429, 5xx, transport failures, and provider 5xx codes retry. Definitive authorization errors select optional `providerRejected`; invalid or oversized JSON selects optional `invalidResponse`. The example leaves optional branches unwired, so they fail the Flow. Public documentation does not state a fixed API quota or charge.

The [customer-fields example](go/examples/customer-fields/README.md) starts `OKKICustomerFields` with `{}`. Its static ConnectionName is `okki`. A real Dex Worker/Client test runs the Flow with a generated provider mock. Live OKKI authentication, token renewal, field retrieval, and Dex Web setup remain unverified.
