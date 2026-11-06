# Zoho CRM connector for Python

Install `dex-connector-zoho-crm==0.1.0`. It requires
`dex-connectors-sdk==0.7.0` and `dex-python-sdk>=1.28.0`.
The package implements all six operations in `connector.yaml` and has no Triggers.

| Operation | Step factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `findRecords` | `new_find_records_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getRecord` | `new_get_record_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `listModifiedRecords` | `new_list_modified_records_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `listModuleFields` | `new_list_module_fields_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `upsertRecord` | `new_upsert_record_step` | `upserted` | `conflict`, `recordRejected`, `providerRejected`, `invalidResponse`, `defect` |
| `updateRecord` | `new_update_record_step` | `updated` | `notFound`, `conflict`, `recordRejected`, `providerRejected`, `invalidResponse`, `defect` |

All provider calls run in Dex Step Execute. Reads use COQL or Zoho CRM v8;
upsert uses a duplicate-check field and update sets absolute field values, so
both writes are safe to repeat. HTTP 408, 429, 5xx, and transport failures
retry. A 401 may refresh and resend once using the generated OAuth declaration.
No operation has an `uncertain` branch. Unwired optional branches fail the Flow.

## Authorization

In Dex Web Connections, create a Zoho CRM connection named `zoho-crm` and
choose the data center where the Zoho organization lives. Follow the
[connector setup instructions](../README.md#zoho-setup) to register Dex Web's
Redirect URI, copy the client ID and secret, and accept the CRM scopes. Dex
stores the access token, refresh token, and returned `api_domain` as
credentials. The connector validates that the API host belongs to the
selected data center before sending a token. The package does not ask for a
manually entered access token or endpoint.

## Example and verification

The [record lookup Flow](examples/record_lookup/flow.py) reads a Deals record
through the static `zoho-crm` connection. Follow its
[run instructions](examples/record_lookup/README.md) to start Dex Web and
its Worker. The test runs that Flow against a real Dex Worker and Client with
an inert HTTP responder:

```sh
uv sync
uv run pytest tests
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40980 uv run pytest examples/record_lookup/integration_test.py
uv run ruff check src tests examples
uv run mypy src examples
```

Start `dexcli dev -dex-port 40980 -web-port 40981 -open=false` before the
integration command and stop it afterwards. The Go-recorded conformance
fixture covers every branch, final-attempt 429 and 5xx retry, transport
failure, duplicate conflict resend, cursor paging, and the generated OAuth
refresh agreement. Live Zoho authorization and provider calls remain
unverified.
