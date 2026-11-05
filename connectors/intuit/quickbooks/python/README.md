# QuickBooks Online Python connector

`dex-connector-intuit-quickbooks` v0.1.0 provides all seven QuickBooks Online operations for Dex Python through the generated operation-specific Step factories and OAuth project connection. It requires `dex-connectors-sdk==0.5.0`.

The [invoice lookup example](examples/get_invoice/) is a runnable Flow using the static `quickbooks-invoice-reader` connection. For authorization setup, exact decimal rules, retry and uncertainty behavior, and provider caveats, see the [connector README](../README.md).

The Go-recorded conformance suite covers provider request and outcome agreement. The example's real Dex integration test uses a local HTTP stand-in. Live Intuit authorization and QuickBooks company calls have not been verified.
