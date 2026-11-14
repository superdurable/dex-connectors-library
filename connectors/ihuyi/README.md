# Ihuyi IP lookup connector

The [Ihuyi IP lookup API](https://www.ihuyi.com/api/ip.html) accepts a public IPv4 address and returns country, province, city, district, and network operator. The connector submits a time-bound MD5 signature to `POST https://api.ihuyi.com/appdev/ip/Submit.json` over HTTPS and filters provider messages and request IDs from the result. Ihuyi is operated by [Shanghai Sirui Information Technology](https://www.ihuyi.com/about.html).

Open [Ihuyi User Center](https://user.ihuyi.com/) > IP查询 > 产品总览 to enable the service and copy its API ID and API KEY. Store the key in Dex Web Connections. Set any required IP allowlist to include the Dex Worker. The connector never returns or logs the key, signature, request body, or raw provider error. Calls may consume the purchased query quota.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. Pass only public IPv4 addresses you are authorized to query. The provider's response calls its province field `porovince`; this connector maps it to `province` in the output. A successful lookup requires a province and city; provider errors, malformed data, oversized bodies, and retryable transport failures have separate outcomes. Real provider authorization and billing have not been verified locally.
