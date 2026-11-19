# OneNET Python connector

`dex-connector-onenet` implements the signed `queryDeviceDetail` Query. Configure the OneNET user ID, base64 AccessKey and product ID from the [OneNET console](https://open.iot.10086.cn) in Dex Web Connections. The Query returns only nonsecret device metadata.

See the [runnable device-detail Flow](examples/device_detail/). Go-recorded conformance scenarios cover signed requests and decisions. The checked-in Dex integration test uses a fake provider; live OneNET authorization and retrieval remain unverified.
