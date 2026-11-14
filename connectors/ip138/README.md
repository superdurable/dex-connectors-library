# iP138 connector

This connector reads [iP138 token usage](https://user.ip138.com/ip/doc/) with `GET https://api.ip138.com/status/`. It returns current-hour requests (`reqs`), purchased hourly calls (`hour`), and package quota (`package`) without querying an IP address. iP138 is operated by [Xiamen Manyou Technology Co., Ltd.](https://user.ip138.com/contact/).

Register at [iP138](https://user.ip138.com/), open iP查询 > 在线API申请, and obtain the secret IP lookup token that iP138 supplies after application or purchase. The official docs do not specify the account page where the delivered token appears; contact iP138 if it is not in your order details. Save it in Dex Web Connections. The provider requires it in the HTTPS query string, so the connector disables redirects and excludes the URL, token, raw message, and unrelated fields from results, receipts, and failures.

Run the [example Flow](go/examples/get-token-usage/) with the generated mock or a user-controlled connection. The provider says HTTP 202 may indicate an invalid token, depleted balance, or malformed input; it is treated as a terminal rejection.
