# NetEase Yunxin Python connector

`dex-connector-netease` implements the `getUserProfile` Query. Configure App Key and App Secret from [Yunxin application management](https://app.yunxin.163.com) in Dex Web Connections. The Query returns only account ID and nickname; private contact and demographic fields are discarded.

See the [runnable user-profile Flow](examples/user_profile/) for Worker and connection setup. Go-recorded conformance scenarios cover complete signed request and decision parity. The checked-in Dex integration test uses a fake provider; real account authorization and profile retrieval have not been verified.
