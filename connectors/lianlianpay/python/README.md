# LianLianPay Python connector

This package implements the `queryPaymentStatus` Query. Configure the static `lianlianpay` connection with the merchant ID and RSA private key described in the [company README](../README.md). The connector signs each order query using RSA and returns only the merchant order number and processing status. It exposes `found`, `providerRejected`, `invalidResponse`, and `defect` branches; only `found` is wired by the runnable example.

Run `uv sync --locked` and `uv run pytest` in this directory. The [payment status example](examples/query_payment_status/) includes a Worker entrypoint and a real Dex integration test. It uses `dexcli dev -open=false` with a mock HTTP provider. Live LianLianPay authorization has not been verified.
