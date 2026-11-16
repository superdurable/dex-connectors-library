# Salesforce connector for TypeScript Dex Flows

This package provides typed Salesforce query, record read, external ID upsert, and record update operations, and from `0.2.0` the `recordModifiedPolled` Trigger. OAuth and JWT bearer refresh use the connector manifest's generated declarations. Configure a Salesforce connection in Dex Web and use a static connection name in a Flow.

## Record change Trigger

`recordModifiedPolled` polls one object through SOQL ordered by `SystemModstamp` and `Id` on the `@superdurable/connectors-sdk` 0.12.0 poll Trigger kit, at behavior parity with the Go connector: the first poll records a baseline and delivers no existing record, recent changes wait for a 15-second settle delay measured on the earlier of the Worker's and Salesforce's clocks, `REQUEST_LIMIT_EXCEEDED` or 429 pauses the connection, and an undecodable cursor or one for another object records a new baseline. Cursor documents and event IDs match the Go connector's byte for byte, and `RecordModifiedEventSchema` decodes Go-written inbox payloads, so Go and TypeScript replicas can share one binding's cursor and inbox.

`newProjectRecordTriggerRunner` is the durable path: it opens the project connection, decodes each saved binding strictly, records each change in the binding's project Trigger inbox before its cursor commits, and returns a poll runner. `newRecordTriggerRunner` takes explicit routes, cursors, and targets, and `newRecordModifiedPolledTrigger` keeps its cursor in memory, so it records a new baseline on every start. `SalesforceClient` rejects a `pollIntervalMs` under 5 seconds or a `pollPageSize` outside 1 to 500; the project connection reads the saved `pollInterval` as Go nanoseconds. The client's `logger` option receives the runner's records, which never carry record values or credentials. The package requires `@superdurable/dex` 1.45.4 or newer.

The [record-changes example](examples/record-changes/README.md) starts one Flow per changed record and includes real Dex Worker and Client integration tests against a local Salesforce stand-in. The [record-lookup example](examples/record-lookup/README.md) reads one record.

Live Salesforce authorization and polling have not been verified for this port.

SOQL `dateTime` bindings accept Go RFC 3339 forms, including a single-digit hour and comma fraction, while rejecting impossible calendar dates. The connector accepts an instance URL according to Go's base URL validation and uses the host transport for the provider connection.
