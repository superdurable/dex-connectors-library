# HubSpot Python connector

This package implements HubSpot CRM search, get, upsert, and update operations for Dex Python Flows and, from 0.2.0, the `crmObjectChanged` poll Trigger at parity with the Go connector. `connector.yaml` defines operation identities, typed Steps, and the private app and OAuth connection contracts. OAuth refresh and shared credential handling are generated from the manifest.

Use `new_project_connection(project, "hubspot-crm")` to bind the connection configured in Dex Web. The runnable Flow in [`examples/read_contact`](examples/read_contact/) demonstrates the connection and operation-specific Step factory.

Run `uv sync --locked`, `uv run pytest`, `uv run ruff check .`, and `uv run mypy src tests examples` from this directory. The package requires `dex-connectors-sdk` 0.12.0 or newer, the first release with the poll Trigger kit. The conformance suite replays Go-recorded HubSpot responses without a real account. With `dexcli dev` listening on port 44400, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:44400 uv run pytest examples/read_contact/integration_test.py` to verify the Flow through a real Dex Worker and Client; `examples/crm_object_changed/integration_test.py` runs the same way. Live HubSpot authorization and provider behavior require a user-controlled Dex Web connection and remain unverified.

## crmObjectChanged Trigger

`crmObjectChanged` polls one object type, `contacts`, `companies`, or `deals`, through HubSpot CRM search and delivers one `CRMObjectChangedEvent` per created or changed record: `object_type`, `change_kind` (`created` or `updated`), `modified_at`, and the `object` as search returned it. It is built on `dex_connectors.sdk.pollingtrigger` and follows the Go connector exactly: the first poll records the newest last-modified date as a baseline and delivers no existing record; later polls search ascending on `lastmodifieddate` (contacts) or `hs_lastmodifieddate`, 100 records per page, re-read a two-minute lag window for late-indexed records, and skip versions already delivered. A 429 or 423 pauses every binding of the connection for `Retry-After` or `X-HubSpot-RateLimit-Interval-Milliseconds`, an exhausted daily limit backs off up to 15 minutes, and a rejected paging offset or unusable cursor records a new baseline with a WARN record. The connection's `pollInterval` (Go nanoseconds in project configuration; at least 5 seconds, 30 by default) sets the delay between polls. The [connector README](../README.md) describes the behavior in full.

The cursor document is the Go connector's JSON byte for byte, and the event ID is the same hash of object type, record ID, and last-modified millisecond, so Go and Python replicas can share one binding's cursor and inbox. `tests/test_crm_object_change_fetcher.py` replays a scenario recorded from the Go fetcher (`tests/fixtures/go-crm-object-change-cursors.json`), compares every cursor token and event ID, and decodes the Go-recorded payloads with `decode_crm_object_changed_event_payload`.

`await new_project_crm_object_trigger_runner(project, connection_name, routes, http_client, logger)` is the durable path: it loads each binding's saved object type, records every change in the binding's project Trigger inbox, and commits the search position in its project poll cursor only after that. Run it with `run(TriggerContext(cancellation))`, wake a binding with `wake_poller`, and close it with `aclose`. From [`examples/crm_object_changed/watcher.py`](examples/crm_object_changed/watcher.py):

```python
runner = await new_project_crm_object_trigger_runner(
    project,
    CONNECTION_NAME,
    [
        ProjectCRMObjectChangedTriggerRoute(
            CRM_OBJECT_CHANGED_TRIGGER_BINDING, target, DELIVERED_PROPERTIES
        )
    ],
    http_client,
    logger,
)
```

`new_crm_object_trigger_runner(connection, routes)` takes targets and cursor stores directly, and `new_crm_object_changed_trigger` builds one binding for `new_trigger`; both keep the cursor in memory unless given a store, so a restart records a new baseline. A Flow declares its static binding with `define_crm_object_changed_trigger_binding` in `get_connector_trigger_bindings`, whose body is a single `return [...]` statement. `decode_crm_object_changed_trigger_configuration` and `validate_crm_object_changed_trigger_configuration` apply the Go connector's strict decoding and validation.

Dex CLI 1.47.3 reads the binding from the example Flow but warns `connector_configuration_ui: Connector ConfigurationUI must be a static literal` for the `objectTypePicker` unit, because its Python analyzer accepts only a dictionary literal while `define_crm_object_changed_trigger_binding` requires a `ConnectorConfigurationUI`. Until the analyzer reads the dataclass form, Dex Web may show the binding without the **Watched object type** picker; save `objectType` in the binding configuration directly or from a Go or TypeScript Flow's binding.

The [crm_object_changed example](examples/crm_object_changed/README.md) starts one Flow per change. Live HubSpot search has not been verified.
