# Wave connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/wave/go@v0.2.0`.

The Go, Python, and TypeScript `getCustomer` Query reads one customer from a Wave business by GraphQL ID. The Go module also lists, creates, renames, and deletes customers. These operations send fixed GraphQL documents to [Wave's public GraphQL API](https://developer.waveapps.com/hc/en-us/articles/360019968212-API-Reference). A missing business or customer selects the optional `notFound` branch when declared. Rate limits, server failures, and transport failures retry.

## Go operations

| Factory | Purpose | Repeat tier |
| --- | --- | --- |
| `NewGetCustomerStep` | Read one customer by business and customer IDs | safe |
| `NewListCustomersStep` | List one 1-based page of customers by business ID | safe |
| `NewCreateCustomerStep` | Create a named customer in a business | limited, maximum three possible sends |
| `NewUpdateCustomerStep` | Replace a customer's name by ID | safe |
| `NewDeleteCustomerStep` | Delete a customer by ID | safe |
| `NewListProductsStep` | List one 1-based page of products by business ID | safe |
| `NewGetProductStep` | Read one product by business and product IDs | safe |
| `NewCreateProductStep` | Create a sold product with a price and income account | limited, maximum three possible sends |
| `NewUpdateProductStep` | Replace a product's name and unit price | safe |
| `NewArchiveProductStep` | Archive a product by ID | safe |
| `NewListInvoicesStep` | List one 1-based page of invoices by business ID | safe |
| `NewGetInvoiceStep` | Read one invoice by business and invoice IDs | safe |
| `NewCreateInvoiceStep` | Create an invoice for one customer and product | limited, maximum three possible sends |
| `NewUpdateInvoiceStep` | Replace an invoice title | safe |
| `NewApproveInvoiceStep` | Approve an invoice by ID | safe |
| `NewDeleteInvoiceStep` | Delete an invoice by ID | safe |
| `NewSendInvoiceStep` | Email an approved invoice to recipients | limited, maximum three possible sends |

Wave's `customerCreate`, `productCreate`, `invoiceCreate`, and `invoiceSend` GraphQL mutations have no documented request idempotency token. After an unknown outcome, these Steps resend within their configured cap and select `uncertain` when the cap is reached; a user-chosen name alone cannot confirm which request created a record. For a sold product, obtain `incomeAccountId` from the Wave API Playground's business accounts query; `unitPrice` is a decimal amount in the business's major currency unit with at most five fractional digits. `sendInvoice` requires an approved invoice and a business with email sending enabled.

## Connection and configuration

For an account you own, open the [Wave Developer Portal](https://developer.waveapps.com/) and choose **Manage Applications**. Create or open your own application and generate a **Full Access Token** as described in [Wave's business-owner guide](https://developer.waveapps.com/hc/en-us/articles/360020596571-Permitted-Use-Wave-Business-Owners). Enter the complete token in the secret `accessToken` field of Dex Web Connections. The token grants broad account access; keep it private and rotate it in the portal when needed. Blank is invalid. This connector currently supports owner-managed tokens, not third-party OAuth connections.

Leave `baseUrl` blank for the Wave production GraphQL root. Override it only for a trusted test gateway; do not append `/graphql/public`. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. In Wave, select the business and customer whose GraphQL IDs you want to read. Pass both IDs as `businessId` and `customerId`; blank IDs are invalid. The [API call guide](https://developer.waveapps.com/hc/en-us/articles/50682329964564-4-Making-API-calls) documents the endpoint and Bearer authorization.

Runnable examples are in [`go/examples/customer-read`](go/examples/customer-read), [`python/examples/customer_read`](python/examples/customer_read), and [`typescript/examples/customer-read`](typescript/examples/customer-read). Provider conformance scenarios are recorded from Go and replayed by Python and TypeScript. Live Wave authorization and provider behavior require a user-controlled token and remain unverified here.
