# Meituan Delivery order-status Flow

Install with `uv sync --locked --project connectors/meituan/python`. Configure the `meituan` connection in Dex Web Connections with AppKey and paired secret from [Meituan Delivery application management](https://peisong.meituan.com/tscc/). From the Python package directory, run `uv run python -m examples.order_status.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start `MeituanOrderStatus` with `{"deliveryId":123456,"meituanOrderId":"MT123456789"}` using an authorized order from the last 90 days. Only `found` is wired; other outcomes fail the Flow for inspection.

The integration test runs this Flow against real Dex with a fake provider. Real Meituan authorization and order retrieval remain unverified.
