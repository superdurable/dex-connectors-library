# Microsoft Excel TypeScript connector

`@superdurable/connector-microsoft-excel` implements `getTableRows`, `getValues`, `updateValues`, and `appendTableRows` using Microsoft Graph workbook APIs. It uses the manifest-generated OAuth refresh declaration and Connector SDK 0.7.0. The connection name in the checked in example is `approvals`.

The [runnable example](examples/approval-decision/README.md) registers four Dex Flows, one per operation. Its integration test runs all four through a real `dexcli dev` Worker and Client against an inert Graph fixture. Go-recorded [conformance scenarios](../../../../conformance/connectors/microsoft/excel/scenarios.json) check provider requests, branches, Retry, uncertainty, and the append heartbeat checkpoint.

Install and verify from this directory:

```sh
npm ci
npm run check
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40660 npm run test:integration
```

Create the Microsoft work or school OAuth connection in Dex Web using the [connector authorization guide](../README.md). The `maxResponseBytes` and `maxCells` settings use their manifest defaults when blank. The TypeScript package does not add a visible configuration field or change Connector Studio, React primitives, or the directory UI.

Live Microsoft authorization and workbook requests have not been verified for this TypeScript port. A separate Connector SDK 0.8.0 follow-up will add the manifest-controlled eight second token exchange deadline used by Go; until then refresh uses the caller's cancellation deadline.
