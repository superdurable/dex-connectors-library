# Sage Intacct connector

`sage-intacct` reads accounts receivable invoices through the [Sage Intacct REST API](https://developer.sage.com/intacct/apis/intacct/1/intacct-openapi/groups/accounts-receivable/groups/invoices/tags/accounts_receivable_invoices/paths/get-accounts-receivable-invoice-key). Go, Python, TypeScript, Java, and Rust implement both `getInvoice` and `listInvoices`.

`getInvoice` takes the invoice's system-assigned `invoiceKey` and returns its `key` and `id`. A 404 selects the optional `notFound` branch. HTTP 408, 429, 5xx, and transport errors return Retry; malformed or oversized JSON selects `invalidResponse`. The Flow fails on an unwired optional branch.

`listInvoices` calls the [invoice list endpoint](https://developer.sage.com/intacct/apis/intacct/1/intacct-openapi/groups/accounts-receivable/groups/invoices/tags/accounts_receivable_invoices/paths/list-accounts-receivable-invoice) and returns one page of invoice keys and IDs. Its result also contains Sage Intacct's opaque `next` link when another page exists. The operation does not follow that link; use Sage Intacct's query service for larger filtered exports.

Set up OAuth at the [Sage App Registry](https://app-registry.sage.com/) with the Dex Web redirect URI and `offline_access`. Dex stores and refreshes access and refresh tokens. The [Sage Intacct OAuth guide](https://developer.sage.com/intacct/docs/1/sage-intacct-rest-api/authorization-and-security/oauth2) explains authorization. Never put credentials in Flow input or Results.

Run the [Go](go/examples/invoice-inspection/README.md), [Python](python/examples/invoice_inspection/README.md), or [TypeScript](typescript/examples/invoice-inspection/README.md) invoice inspection Flow with the static connection name `sage-intacct`. These runnable Flows are the source for application API usage.

Live Sage authorization and invoice retrieval require a user-controlled Intacct account and have not been verified in this checkout.


## Java and Rust examples

The [Java Worker](java/examples/io/superdurable/dexconnectors/sageintacct/InvoiceDirectoryWorker.java) registers both invoice Flows. Run `./gradlew invoiceDirectoryWorker` from `java/` with an unexpired `SAGE_INTACCT_ACCESS_TOKEN` supplied by your credential manager. The standalone Worker uses this token for its lifetime. Hosted applications use `SageIntacctClient.fromProject` with the named renewable connection.

The [Rust lookup](rust/examples/get_invoice.rs) and [Rust listing](rust/examples/list_invoices.rs) Workers load the named connection from the Dex project. With `DEX_CONNECTOR_CONNECTIONS_FILE` pointing to the Dex Web connections file, run `cargo run --example get_invoice` or `cargo run --example list_invoices` from `rust/`.

Set `DEX_FLOW_SERVICE_ADDRESS` to the running Dex service. Lookup takes `{"invoiceKey":"443"}` with an accessible invoice key. Listing takes `{}`. To exercise retries, OAuth rotation, and both results against a provider fixture, run `./gradlew test dexDevTest` from `java/` and `cargo test --all-targets` from `rust/`.

Generate and lint both native examples from the connector directory.

```sh
for flow in GetInvoiceFlow ListInvoicesFlow; do
  dexcli visualize "java/examples/io/superdurable/dexconnectors/sageintacct/${flow}.java" --language java --schema-version 2.0 --lint app --open=false --json --out "/tmp/sage-${flow}"
done
for flow in get_invoice list_invoices; do
  dexcli visualize "rust/examples/${flow}.rs" --language rust --schema-version 2.0 --lint app --open=false --json --out "/tmp/sage-${flow}"
done
```
