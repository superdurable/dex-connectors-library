# Yunda Express connector

`GetWaybillBalance` reads the remaining free and paid ordinary electronic waybills in a Yunda Baima account. The connector signs a JSON request with the application secret and returns only the two counts.

Create an application at [Yunda Open Platform](https://open.yundaex.com/) and get its app-key and app-secret. Your cooperating Yunda outlet supplies the Baima partner_id and integration secret. Enter all four values in a Dex Web Connection named `yunda`; the two secrets and identifiers are stored as secret fields. The operation has no input and uses the `common` waybill type.

Yunda's [authentication rules](https://yundaex.apifox.cn/doc-345775) and [balance API](https://yundaex.apifox.cn/api-7039547) define the request and response. The checked-in [runnable Flow](go/examples/get-waybill-balance/) uses a real Dex Worker with a mock by default. Live Yunda authorization and Baima balance behavior remain unverified.
