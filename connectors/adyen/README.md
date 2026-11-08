# Adyen connector

The Go, Python, and TypeScript `getPaymentLink` Query reads a Checkout payment link's ID, status, and amount currency. Shopper and payment details stay out of Dex Results. A completed link is useful for payment workflow inspection; the operation does not retrieve a payment by PSP reference.

## Connect

In the [Adyen Customer Area](https://ca-test.adyen.com), select your company account and open **Developers > API credentials > Payments > credential username > Server settings > Authentication > API key**. Generate and copy the secret key into Dex Web `api_key`, then save the credential. Grant its **Checkout webservice role** under Permissions. The key is sent in `X-API-Key`. See [API credentials](https://docs.adyen.com/development-resources/api-credentials/) and [roles](https://docs.adyen.com/development-resources/api-credentials/roles).

Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults in test. Live requires a live API key and a URL with your company prefix from **Developers > API URLs > Prefix**: `https://{PREFIX}-checkout-live.adyenpayments.com/checkout/v72`. Find the non-secret link ID on the Payment Links page or in the create-link response and pass it as `paymentLinkId`, for example `PL50C5F751CED39G71`. See [Get a payment link](https://docs.adyen.com/api-explorer/Checkout/latest/get/paymentLinks/%28linkId%29) and the [Checkout API overview](https://docs.adyen.com/api-explorer/Checkout/latest/overview).

## Examples and verification

Run the [Go](go/examples/payment-link-inspection/README.md), [Python](python/examples/payment_link_inspection/README.md), or [TypeScript](typescript/examples/payment-link-inspection/README.md) Flow with the static `adyen` connection. Go-recorded conformance checks requests, branches, retries, and outputs across the three languages. Real Dex integration tests use fixture providers. Real Adyen authorization and link retrieval remain unverified.
