# STO Express Tracking

The Go `queryWaybillTrace` Query reads scan type, scan time, and scan branch name for a single authorized STO waybill. The [official API definition](https://open.sto.cn/#/apiDocument/STO_TRACE_QUERY_COMMON) defines `waybillNoList`, the per-waybill result map, and a maximum of 20 waybills per request. This connector sends one waybill per call and omits names, phone numbers, pickup codes, addresses, and detailed memos from its result.

## Connection

At [STO Open Platform](https://open.sto.cn), complete onboarding, then use **控制台 → 应用管理** to create an application. Subscribe to `STO_TRACE_QUERY_COMMON`, submit it for review, and activate it for production. In the application's details, copy App Key, secret App Secret, and the application resource code to Dex Web Connections. The [onboarding guide](https://open.sto.cn/#/help/mp2qqe) and [gateway guide](https://open.sto.cn/#/help/tz5gl0) describe the setup.

The gateway accepts HTTPS POST form data at `https://cloudinter-linkgateway.sto.cn/gateway/link.do`. The connector sets the fixed `api_name`, `to_appkey`, and `to_code`; it signs the exact JSON `content` as `base64(md5(content + appSecret))` per the [official signature guide](https://open.sto.cn/#/help/gtknvy). App Secret remains local. HTTP 429, HTTP 5xx, provider retry hints, gateway timeout/availability codes, and transport failures retry. Definite provider errors select optional `providerRejected`; malformed or oversized responses select optional `invalidResponse`. The example leaves optional branches unwired, so those outcomes fail the Flow. The public API detail does not publish a fixed quota or price; check the subscribed application's terms before live use.

## Runnable Flow

The [waybill-trace example](go/examples/waybill-trace/README.md) starts `STOWaybillTrace` with `{"waybillNo":"884000400421364"}`. Its static ConnectionName is `sto`, and it stores only minimized tracking data in a Dex Attribute. See the example README for the Worker command, strict FDG generation, and real Dex mock test.
