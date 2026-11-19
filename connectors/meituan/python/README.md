# Meituan Delivery Python connector

`dex-connector-meituan` implements the `queryOrderStatus` Query. Configure the AppKey and paired secret from [Meituan Delivery application management](https://peisong.meituan.com/tscc/) in Dex Web Connections. The Query returns delivery IDs, status and operation time without rider information.

See the [runnable order-status Flow](examples/order_status/) for Worker and connection setup. Go-recorded conformance scenarios cover signed request and decision parity. The checked-in Dex integration test uses a fake provider; real account authorization and order retrieval have not been verified.
