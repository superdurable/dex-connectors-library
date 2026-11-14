# IPDataCloud IP lookup connector

The [IPDataCloud v2 query API](https://help.ipdatacloud.com/doc/ipguishudi/?tp=3&type=2) accepts an IP address and returns a `data.location` object. This connector queries public IPv4 addresses and maps country, province, city, district, and ISP to a bounded output. IPDataCloud is operated by [江苏舍恩伯格信息科技有限公司](https://help.ipdatacloud.com/doc/ipguishudi/?tp=3&type=2).

Sign in at the [IPDataCloud center](https://myip.ipdatacloud.com/), subscribe to the global IP location API, then open 个人中心 > API管理 to find its product KEY. Store it in Dex Web Connections. The provider's documented HTTPS request puts the KEY in a query parameter; the connector excludes it and raw provider messages from results and errors. Calls may consume purchased query quota.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. Pass only public IPv4 addresses you are authorized to query. Provider errors, malformed data, oversized bodies, and retryable transport failures have separate outcomes. Real provider authorization and billing have not been verified locally.
