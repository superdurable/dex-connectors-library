# Haimou IP lookup connector

The [亥牟数据 IP 归属地 API](https://hmkj4399.com/detail-ip.html) accepts a public IPv4 address through `GET /v1/ip` and returns country, province, city, district, and ISP. The service is operated by [杭州亥牟科技有限公司](https://hmkj4399.com/index.html). This connector returns only coarse location and operator fields and excludes the provider response and credential.

Sign in at [亥牟数据](https://hmkj4399.com/), complete provider account verification, then open **控制台 > 应用管理** and create an application authorized for IP 归属地. Copy its secret apikey into Dex Web Connections. The provider requires this key in the HTTPS query string, so avoid logging request URLs. Each query may consume provider quota; reset a disclosed key in application details.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. Pass only public IPv4 addresses you are authorized to query. Provider errors, malformed data, oversized bodies, and retryable transport failures have separate outcomes. Real provider authorization and billing have not been verified locally.
