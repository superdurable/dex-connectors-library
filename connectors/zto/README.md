# ZTO Express Tracking

The Go `queryWaybillTrace` Query reads scan type, millisecond timestamp, and scan site name for a ZTO waybill linked to the application's electronic-waybill account. The [official API definition](https://open.zto.com/#/documentsDetail?resourceGroup=10&apiName=zto.merchant.waybill.track.query) defines `billCode`, the result list, and E403/E404/E409/E413/E500 outcomes. The connector omits descriptions, phone numbers, signers, and staff details because those fields can contain personal information.

## Connection

At [ZTO Open Platform](https://open.zto.com), complete enterprise certification, then use **控制台 → 业务对接 → 开发者对接** to create an application. In **应用详情 → 应用信息**, copy AppKey and secret AppSecret to Dex Web Connections. In **添加服务**, add `zto.merchant.waybill.track.query`; in **业务参数配置**, bind the electronic-waybill account to the application. Publish and wait for production approval. The [developer guide](https://open.zto.com/#/documents?menuId=59) explains these steps. The connector does not use telephone-digit authorization, so an unbound account yields a provider rejection.

The [interface and signature rules](https://open.zto.com/#/documents?menuId=3) require HTTPS POST JSON with `x-appKey` and `x-dataDigest = base64(md5(exactBody + appSecret))`. The AppSecret stays local. HTTP 429, HTTP 5xx, provider E500, and transport failures retry. Definite provider errors select optional `providerRejected`; malformed or oversized responses select optional `invalidResponse`. The example leaves optional branches unwired, so they fail the Flow. ZTO does not publish a fixed quota or fee for this API in its public contract; check the application's service terms before live use.

## Runnable Flow

The [waybill-trace example](go/examples/waybill-trace/README.md) starts `ZTOWaybillTrace` with `{"billCode":"73100059800035"}`. Its static ConnectionName is `zto`, and it stores only the minimized tracking result in a Dex Attribute. See the example README for the Worker command, strict FDG generation, and real Dex mock test.
