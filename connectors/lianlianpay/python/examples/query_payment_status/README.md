# LianLianPay payment status Flow

`worker.py` loads the static `lianlianpay` project connection from Dex configuration and runs `PaymentStatusFlow`. The Flow uses `new_query_payment_status_step` and stores the merchant order status in a Dex Attribute. Generate the Flow Definition Graph with `dexcli visualize --open=false` from this example's Worker entrypoint. Run `integration_test.py` against `dexcli dev -open=false` to verify 503 retry and persisted completion with a mock LianLianPay HTTP service.
