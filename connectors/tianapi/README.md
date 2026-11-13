# TianAPI connector

This connector calls TianAPI's official [holiday API](https://www.tianapi.com/apiview/139), `GET https://apis.tianapi.com/jiejiari/index`, for one Chinese calendar date. TianAPI is operated by [Shanghai Jueke Information Technology Co., Ltd.](https://www.tianapi.com/about.html). The [OpenAPI definition](https://www.tianapi.com/openapi/jiejiari.json) documents `date`, `type=0`, the `key` query parameter, and the `code`/`result.list` response.

Register at [TianAPI](https://www.tianapi.com/), apply for 节假日, then copy your secret key from 控制台 > 数据管理 > 我的密钥key into Dex Web Connections. The [official demo](https://www.tianapi.com/demo/api/jiejiari.html) shows this path. The provider requires the key in the HTTPS query string; the connector disables redirects and excludes the URL, key, raw message, and unrelated holiday fields from outputs and failures.

TianAPI lists 100 free calls per day for ordinary members. Successful requests count toward quota. `dayCode` is 0 workday, 1 holiday, 2 weekend, or 3 makeup workday; `isNotWork` is 0 working or 1 resting. Use `isNotWork` to decide whether to work, as the official page recommends. Run the [example Flow](go/examples/get-holiday-status/) with the generated mock or a user-controlled connection.
