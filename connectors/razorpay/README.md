# Razorpay connector

The Go, Python, and TypeScript `getPayment` Query reads a payment's ID, status, amount in currency subunits, and currency code. Customer contact and payment method details are excluded from Dex Results. Razorpay's HTTP 400 `BAD_REQUEST_ERROR` with description `The id provided does not exist`, returned for an unknown ID or one from another account or test/live mode, selects `notFound`; other rejections select `providerRejected`.

## Connect

In the [Razorpay Dashboard](https://dashboard.razorpay.com), choose Test or Live Mode, then open **Account & Settings > API Keys > Generate Key**. Copy the Key ID (beginning `rzp_test_` or `rzp_live_`) and the paired Key Secret into Dex Web `key_id` and `key_secret`. Both are stored as secrets and sent only in HTTP Basic authentication. Blank is invalid. Regenerate the pair on the same page to rotate it; do not mix Test and Live keys. See [Razorpay API keys](https://razorpay.com/docs/server-integration/python/test-app/).

Leave `baseUrl` blank for `https://api.razorpay.com/v1`. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; blank uses their displayed defaults. Find the non-secret payment ID beginning `pay_` in Dashboard Payments or an API response and pass it as `paymentId`; blank is invalid. See [Fetch a Payment With ID](https://razorpay.com/docs/api/payments/fetch-with-id/).

## Examples and verification

Run the [Go](go/examples/payment-inspection/README.md), [Python](python/examples/payment_inspection/README.md), or [TypeScript](typescript/examples/payment-inspection/README.md) payment inspection Flow with the static `razorpay` connection. Go-recorded conformance checks exact requests, branches, retries, and outputs across all three languages. Local integration tests exercise real Dex with fixture providers. Real Razorpay authorization and retrieval remain unverified.
