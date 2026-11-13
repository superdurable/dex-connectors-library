# YikeAPI IP Lookup

The [official IP location API](https://www.yikeapi.com/index/ip) accepts a public IPv4 address through HTTPS `GET /ip/` and returns country, province, city, and district. The provider identifies itself as 济南易客云网络科技有限公司 in its [contact page](https://yikeapi.com/index/contact).

Register at [YikeAPI](https://www.yikeapi.com), open 用户中心, find `appid` and `appsecret`, and add a `yikeapi` connection in Dex Web. Activate the IP address service and check account quota. Both credentials are secrets required in the provider's HTTPS query, so avoid logging request URLs or request objects. The connector excludes the credentials and raw response from Flow output and errors. Leave timeout and maxResponseBytes blank for their displayed defaults.

The [runnable Dex example](go/examples/lookup-ip-location/) uses the static `yikeapi` connection. Its default mock is synthetic and does not verify live provider authorization or response data.
