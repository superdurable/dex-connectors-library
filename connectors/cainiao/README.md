# Cainiao electronic-waybill connector

The Go `queryWaybillTrace` Query reads logistics events for a Cainiao electronic waybill through the [official TOP API](https://developer.alibaba.com/docs/api.htm?apiId=27230). It requires the carrier `cp_code` and electronic waybill number. Ordinary carrier tracking numbers may have no data. The API is labeled free but requires application permission and merchant authorization; check current quotas in the TOP console.

## Connect

At the [Alibaba developer console](https://developer.alibaba.com), open **控制台 → 开发 → 我的应用 → 应用管理** and select an application with permission for `cainiao.waybill.logisticstrace.get`. Copy its AppKey and secret AppSecret into Dex Web Connections. Have the merchant authorize the application, then obtain its SessionKey (access token) using the console authorization tool for development or your existing TOP authorization flow for production. Store SessionKey as a secret Dex credential and renew it before expiry. The [authorization guide](https://developer.alibaba.com/docs/doc.htm?articleId=102635&docType=1) explains token lifetime. Leave transport settings blank for their displayed defaults.

TOP's [signature rules](https://developer.alibaba.com/docs/doc.htm?articleId=101617&docType=1) require HMAC-MD5 over sorted request fields. The connector signs each request, sends the token only over HTTPS, and never includes the AppSecret in the request. HTTP 429, HTTP 5xx, and transport failures retry; definite provider errors select optional `providerRejected`, and malformed or oversized responses select optional `invalidResponse`. The example leaves optional branches unwired, so those outcomes fail the Flow.

The runnable Flow is in [`go/examples/waybill-trace`](go/examples/waybill-trace). Provider tests cover signing and response classifications. Live merchant authorization, application permission, and production trace data remain unverified without user-controlled credentials and a qualifying waybill.
