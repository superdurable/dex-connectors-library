# Sage Intacct invoice inspection

This Go Flow calls `sageintacct.NewGetInvoiceStep` for one invoice key and stores the selected `InvoiceSummary` in a Dex Attribute before completion. The static Dex Web connection name is `sage-intacct`.

From the repository root, generate the strict FDG 2.0 graph:

```sh
dexcli visualize connectors/sage/intacct/go/examples/invoice-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/sage-intacct-fdg --lint app
```

Create a Sage Intacct connection in Dex Web as described in [the connector README](../../../README.md), then run `go run ./examples/invoice-inspection` from `connectors/sage/intacct/go`. Start `SageIntacctInvoiceInspection` with JSON input such as `{"invoiceKey":"443"}`. The key must be the system-assigned key of an invoice accessible to the authorized user. The example's mock integration test exercises the same Flow against a local Dex stack without live Sage credentials.
