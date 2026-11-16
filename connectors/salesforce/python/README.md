# Salesforce connector for Python Dex Flows

This package provides typed Salesforce query, record read, external ID upsert, and record update operations, and from `0.2.0` the `recordModifiedPolled` Trigger. It uses the connector manifest's generated OAuth and JWT bearer refresh declarations. Configure a Salesforce connection in Dex Web, then use the static connection name in a Flow.

## Record change Trigger

`recordModifiedPolled` polls one object through SOQL ordered by `SystemModstamp` and `Id` on the `dex-connectors-sdk` 0.12.0 poll Trigger kit, at behavior parity with the Go connector: the first poll records a baseline and delivers no existing record, recent changes wait for a 15-second settle delay measured on the earlier of the Worker's and Salesforce's clocks, `REQUEST_LIMIT_EXCEEDED` or 429 pauses the connection, and an undecodable cursor or one for another object records a new baseline. Cursor documents and event IDs match the Go connector's byte for byte, and `decode_record_modified_event_payload` decodes Go-written inbox payloads, so Go and Python replicas can share one binding's cursor and inbox.

`new_project_record_trigger_runner` is the durable path: it opens the project connection, decodes each saved binding with the generated strict decoder, records each change in the binding's project Trigger inbox before its cursor commits, and returns a `ProjectRecordTriggerRunner` to `run` and `aclose`. `new_record_trigger_runner` takes explicit routes, cursors, and targets, and `new_record_modified_polled_trigger` keeps its cursor in memory, so it records a new baseline on every start. `Client` rejects a `poll_interval_seconds` under 5 or a `poll_page_size` outside 1 to 500, and its `logger` receives the runner's records, which never carry record values or credentials.

The [record_changes example](examples/record_changes/README.md) starts one Flow per changed record and includes real Dex Worker and Client integration tests against a local Salesforce stand-in. The [record_lookup example](examples/record_lookup/README.md) reads one record.

Live Salesforce authorization and polling have not been verified for this port.
