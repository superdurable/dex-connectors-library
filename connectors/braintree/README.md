# Braintree connector

The Go, Python, and TypeScript `getTransaction` Query reads one transaction's GraphQL ID and status using Braintree's [node query](https://developer.paypal.com/braintree/graphql/guides/node_query/). A null node or a node of another type selects optional `notFound`; definite GraphQL errors select `providerRejected`. HTTP 429, server failures, and transport failures retry. The operation only reads a transaction and never moves money.

## Connection and configuration

Open the matching [Braintree sandbox or production Control Panel](https://developer.paypal.com/braintree/articles/control-panel/important-gateway-credentials), then **gear icon > API > API Keys**. Generate an API key if necessary. Copy its Public Key into `publicKey`, select **View** in the Private Key column, and copy the secret into `privateKey` in Dex Web Connections. Both are required and must come from the same environment. Dex sends their Basic encoding only in the provider request. Never enter a tokenization key or a merchant ID in these fields.

Leave `baseUrl` blank for the sandbox GraphQL root. For production, enter `https://payments.braintree-api.com`. Leave `apiVersion`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. The [Braintree GraphQL request guide](https://developer.paypal.com/braintree/graphql/guides/making_api_calls/) specifies the API version date and request headers. Pass a GraphQL transaction ID as `transactionId`; blank is invalid. A legacy SDK transaction ID may not be the same value.

Runnable examples are in [`go/examples/transaction-read`](go/examples/transaction-read), [`python/examples/transaction_read`](python/examples/transaction_read), and [`typescript/examples/transaction-read`](typescript/examples/transaction-read). Provider conformance scenarios are recorded from Go and replayed by Python and TypeScript. Live Braintree authorization and provider behavior require user-controlled sandbox or production keys and remain unverified here.
