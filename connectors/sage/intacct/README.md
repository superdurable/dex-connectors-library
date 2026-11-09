# Sage Intacct connector

`sage-intacct` reads accounts receivable invoices through the [Sage Intacct REST API](https://developer.sage.com/intacct/apis/intacct/1/intacct-openapi/groups/accounts-receivable/groups/invoices/tags/accounts_receivable_invoices/paths/get-accounts-receivable-invoice-key). Version `v0.1.0` implements `getInvoice` in Go, Python, and TypeScript.

`getInvoice` takes the invoice's system-assigned `invoiceKey` and returns its `key` and `id`. A 404 selects the optional `notFound` branch. HTTP 408, 429, 5xx, and transport errors return Retry; malformed or oversized JSON selects `invalidResponse`. The Flow fails on an unwired optional branch.

Set up OAuth at the [Sage App Registry](https://app-registry.sage.com/) with the Dex Web redirect URI and `offline_access`. Dex stores and refreshes access and refresh tokens. The [Sage Intacct OAuth guide](https://developer.sage.com/intacct/docs/1/sage-intacct-rest-api/authorization-and-security/oauth2) explains authorization. Never put credentials in Flow input or Results.

Run the [Go](go/examples/invoice-inspection/README.md), [Python](python/examples/invoice_inspection/README.md), or [TypeScript](typescript/examples/invoice-inspection/README.md) invoice inspection Flow with the static connection name `sage-intacct`. These runnable Flows are the source for application API usage.

Live Sage authorization and invoice retrieval require a user-controlled Intacct account and have not been verified in this checkout.
