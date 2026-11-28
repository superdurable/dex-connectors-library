# FreshBooks connector for TypeScript

This package implements `getInvoice` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and retry behavior. The [runnable invoice Flow](examples/invoice/README.md) uses the static `freshbooks` Dex Web connection.

Run `npm ci` and `npm run check` from this directory. Live FreshBooks authorization and Dex Web field rendering have not been verified.

The same Worker registers the [invoice page Flow](examples/invoice/list-flow.ts). Supply an accounting account ID and a one-based page number. A zero or omitted page selects page one. The Flow returns at most 100 invoices along with the provider’s page count and total.
