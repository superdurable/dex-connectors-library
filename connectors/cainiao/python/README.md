# Cainiao Python connector

`dex-connector-cainiao` exposes `queryWaybillTrace` as a Dex Query Step. The
[official TOP API](https://developer.alibaba.com/docs/api.htm?apiId=27230)
requires an electronic waybill code and carrier `cp_code`; an ordinary carrier
tracking number may have no trace. The connector signs a form request with
TOP HMAC-MD5 and never sends the AppSecret. It keeps the SessionKey in the
connection, outside Flow input and Attributes.

In [Alibaba Developers](https://developer.alibaba.com), open **控制台 → 开发 →
我的应用 → 应用管理** to select an app permitted to call
`cainiao.waybill.logisticstrace.get`. Put its AppKey and secret AppSecret in
Dex Web Connections. Ask the merchant to authorize the app, then use **授权工具**
or the existing TOP authorization flow to obtain a secret SessionKey. The
[TOP token guide](https://developer.alibaba.com/docs/doc.htm?articleId=102635&docType=1)
explains renewal. Leave endpoint, timeout, and response size blank for the
manifest defaults.

The [runnable Python Flow](examples/waybill_trace/) reads and persists one
bounded trace through the static `cainiao` connection. Provider conformance
replays Go-recorded requests and outcomes. Real merchant authorization and a
production electronic waybill have not been verified locally.
