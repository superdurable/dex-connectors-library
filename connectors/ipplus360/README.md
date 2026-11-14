# IPPlus360 IP lookup connector

The [IPPlus360 district-level IPv4 API](https://mall.ipplus360.com/pros/IPVFourGeoAPI) maps a public IPv4 address to country, province, city, district, and ISP. This connector uses the documented HTTPS endpoint `GET /ip/geo/v1/district/` and returns only those coarse fields. IPPlus360 is operated by 郑州埃文科技有限公司.

Open the [IPPlus360 product page](https://mall.ipplus360.com/pros/IPVFourGeoAPI), activate the **区县级** IPv4 product, then sign in at [个人中心 > 我的订单](https://account.ipplus360.com/setting) to copy that product's KEY. Store it in Dex Web Connections. The provider's documented request puts the KEY in a query parameter; the connector omits it and raw provider messages from results and errors. Successful lookups may consume purchased quota.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. Pass only public IPv4 addresses you are authorized to query. Provider errors, malformed data, oversized bodies, and retryable transport failures have separate outcomes. Real provider authorization and billing have not been verified locally.
