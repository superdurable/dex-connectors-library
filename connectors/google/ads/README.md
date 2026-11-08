# Google Ads connector

The Go, Python, and TypeScript `listAccessibleCustomers` Queries return customer resource names directly accessible to the connected Google identity. They use the [v26 CustomerService endpoint](https://developers.google.com/google-ads/api/rest/common/others) and accept a local result limit so a large account list does not enter Dex Flow state unexpectedly. The runnable [Go](go/examples/customers/README.md), [Python](python/examples/customers/README.md), and [TypeScript](typescript/examples/customers/README.md) Flows use the static `google-ads` Dex Web connection. Campaign reporting and mutations remain outside this initial capability.

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), enable Google Ads API, configure OAuth consent with the `adwords` scope, and create a Web application client with the exact redirect URI displayed by Dex Web. Google Ads now assigns API access to the Cloud project: Test access covers test accounts; production accounts require an approved access level. [Developer tokens are no longer required](https://developers.google.com/google-ads/api/docs/api-policy/developer-token) for this API.

Live Google consent, token refresh, Google Ads reads, production access, and Dex Web field rendering have not been verified in this checkout.
