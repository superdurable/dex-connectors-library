# Huoban connector

This connector calls the current [Huoban OpenAPI workspace list](https://huobanyun.apifox.cn/api-17851166), `GET https://api.huoban.com/openapi/v1/space/list`, and returns only authorized workspace IDs and names. It discards cover images and never reads table records or workspace members.

An enterprise administrator creates a secret API Key at [伙伴云](https://app.huoban.com/v5) under **企业设置 > 企业后台 > 接口授权 > 创建 API Key**. The [official API Key guide](https://openapi.huoban.com/doc-661256) says a paid enterprise version is needed and the key can be restricted to selected workspaces and trusted IPs. Add the key to Dex Web Connections without `Bearer`; the connector supplies the [`Open-Authorization` header](https://openapi.huoban.com/doc-661256). A blank key prevents requests.

The [official OpenAPI introduction](https://openapi.huoban.com/) specifies HTTPS and a default limit of 10 requests per second per key, plus a plan-specific per-minute limit. The fixed host supports 伙伴云 accounts; 伙伴钉 uses another host and is outside this connector's scope. Run the [example Flow](go/examples/list-spaces/) with the generated mock or a user-controlled connection.
