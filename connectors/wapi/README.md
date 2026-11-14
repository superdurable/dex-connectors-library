# Wapi vehicle brand prices

`ListVehicleBrands` reads brand names, IDs, and maintenance-record query prices in Chinese fen from the [official brand and price API](https://www.wapi.cn/api_detail/170/324.html?format=md). [Wapi's about page](https://www.wapi.cn/help_42.html) identifies 厦门训数科技有限公司 as the Chinese operator. This connector calls the documented read-only, unbilled subinterface and never orders maintenance records.

At [Wapi](https://www.wapi.cn), open 会员中心 > 我的应用, create an app, add 维修保养记录精准版, and copy its numeric AppID and 32-character secret to Dex Web Connections. Select MD5 signature verification and authorize your egress IP if required. The connector signs `appid`, `format=json`, and a Unix timestamp in seconds; the secret is excluded from the Flow. Leave timeout and maxResponseBytes blank for their displayed defaults.

The [runnable vehicle-brand Flow](go/examples/vehicle-brands/) uses `ConnectionName = "wapi"`. Run its worker with `go run .`, then start `WapiVehicleBrands` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow. Real provider authorization is unverified without a customer-controlled app.
