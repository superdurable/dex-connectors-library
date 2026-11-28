# Sage Intacct connector for TypeScript

This package implements `getInvoice` and `listInvoices` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and retry behavior. The [invoice lookup Flow](examples/invoice-inspection/README.md) and [listing Flow](examples/invoice-listing/README.md) use the static `sage-intacct` Dex Web connection.

Run `npm ci` and `npm run check` from this directory. Live Sage Intacct authorization and Dex Web field rendering have not been verified.
