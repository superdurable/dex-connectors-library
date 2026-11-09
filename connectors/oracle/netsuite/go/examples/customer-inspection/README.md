# Oracle NetSuite customer inspection

This runnable Go Flow calls `oraclenetsuite.NewGetCustomerStep` for one numeric customer internal ID and stores the `CustomerSummary` in a Dex Attribute. Its static Dex Web connection name is `oracle-netsuite`.

From the repository root, generate the strict FDG 2.0 graph:

```sh
dexcli visualize connectors/oracle/netsuite/go/examples/customer-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/oracle-netsuite-fdg --lint app
```

Create an Oracle NetSuite connection in Dex Web as described in [the connector README](../../../README.md), then run `go run ./examples/customer-inspection` from `connectors/oracle/netsuite/go`. Start `OracleNetSuiteCustomerInspection` with input `{"customerId":"107"}`, replacing `107` with a customer internal ID visible to the authorized role. The mock integration test runs the same Flow against a local Dex stack without live NetSuite credentials.
