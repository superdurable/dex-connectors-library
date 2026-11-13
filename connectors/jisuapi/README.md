# JisuAPI connector

This connector calls the official [administrative-area province API](https://www.jisuapi.com/api/area/), `GET https://api.jisuapi.com/area/province`, and returns only province IDs and names. JisuAPI is operated by [Hangzhou Jisu Internet Technology Co., Ltd.](https://www.jisuapi.com/about/).

Register at [极速数据](https://www.jisuapi.com/), apply for 全国省市县行政区划分, and copy the secret `appkey` from 个人中心 into Dex Web Connections. The [official integration guide](https://m.jisuapi.com/news/detail/580) also explains optional IP whitelisting. The provider requires `appkey` in the HTTPS query string; this connector disables redirects and excludes the URL, key, and raw response message from results and errors.

The official product page lists 100 free calls per day for free members, with higher membership allowances. A successful status 0 generally counts as a call according to the integration guide. Run the [example Flow](go/examples/list-provinces/) with the generated mock or a user-controlled connection.
