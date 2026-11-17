# Oracle NetSuite customer inspection

This runnable Go example registers `OracleNetSuiteCustomerInspection` to read one customer, `OracleNetSuiteCustomerListing` to list one customer page, and `OracleNetSuiteVendorListing` to list one vendor page. The Flows call operation-specific Step factories and store typed results in Dex Attributes. Their static Dex Web connection name is `oracle-netsuite`.

From the repository root, generate the strict FDG 2.0 graph:

```sh
dexcli visualize connectors/oracle/netsuite/go/examples/customer-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/oracle-netsuite-fdg --lint app
```

Create an Oracle NetSuite connection in Dex Web as described in [the connector README](../../../README.md), then run `go run ./examples/customer-inspection` from `connectors/oracle/netsuite/go`. Start `OracleNetSuiteCustomerInspection` with input `{"customerId":"107"}`, replacing `107` with a customer internal ID visible to the authorized role. Start `OracleNetSuiteCustomerListing` or `OracleNetSuiteVendorListing` with input `{"limit":100,"offset":0}`; limit is a page size from 1 to 1000 and offset is a zero-based multiple of limit. The mock integration test runs all three Flows against a local Dex stack without live NetSuite credentials.
