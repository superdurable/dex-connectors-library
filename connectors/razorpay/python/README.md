# Razorpay Python connector

Implements `getPayment`, returning payment ID, status, amount in currency subunits, and currency code. Go-recorded conformance checks exact requests, branches, retries, and outputs.

Create a Razorpay Test or Live key pair under **Account & Settings > API Keys**, then save its Key ID and Key Secret as `key_id` and `key_secret` in Dex Web Connections. Leave `baseUrl` blank for the default endpoint. See the [connection guide](../README.md) and [runnable example](examples/payment_inspection/README.md). Live provider authorization remains unverified.
