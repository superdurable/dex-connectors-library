# DocuSign eSignature Python connector

This package implements `createEnvelopeFromTemplate`, `getEnvelope`,
`listEnvelopeRecipients`, `voidEnvelope`, and `downloadCombinedDocument` for
Dex Python Flows, and the `envelopeEventReceived` DocuSign Connect Trigger.
The manifest owns operation identities, retry policies, branches, configuration,
and OAuth methods. The Python package version is `v0.2.1` and requires
`dex-connectors-sdk>=0.11.0`.

`new_project_connection` loads the static connection name from Dex project
configuration. It uses the generated OAuth credential decoder and refresh
driver for production and developer accounts. Each provider call resolves the
current access token, reads `/oauth/userinfo` to choose the account and regional
API host, and rejects a host outside the selected environment. The PDF operation
returns a digest, byte count, and optional store location; PDF bytes never enter
a Dex Result. `CombinedDocumentStore` accepts bounded PDF bytes outside Dex and
must be idempotent and atomic.

## envelopeEventReceived Trigger

The Trigger runs on the Python Connector SDK webhook endpoint
(`dex_connectors.sdk.webhooktrigger`) at behavior parity with the Go
connector. Each connection has one shared Connect endpoint, an ASGI 3
application. It answers 413 for a body above `connectMaxBodyBytes` before the
signature is checked, 503 while no binding runs or the connection has no
`connect_hmac_key` (so Connect retries), and 400 unless some
`X-DocuSign-Signature-<n>` header equals the body's base64 HMAC-SHA256 under the
key with its quotes removed, compared in constant time. A JSON SIM
`envelope-completed`, `envelope-declined`, or `envelope-voided` event becomes
one `EnvelopeEvent` whose Trigger event ID is `<envelope ID>:<event>`, with the
envelope summary's lowercase status, void reason, and text custom fields when
the Connect configuration includes them. Keys match case-insensitively, as Go's
`encoding/json` decodes them. Other Connect events answer 200 unrecorded; XML or
malformed bodies answer 400. A binding's saved `events` list (empty accepts all
three) filters what it records.

`new_project_envelope_event_received_endpoint_runner` records each event in the
binding's project Trigger inbox before Connect receives 200, and its `run`
replays pending events after a restart, including ones a Go replica recorded.
Python stores a generation time with microsecond precision; DocuSign's
seven-digit fractions are truncated, which changes no event ID.
[`examples/envelope_outcome/receiver.py`](examples/envelope_outcome/receiver.py)
wires it to a Flow-start target:

```python
target = new_dex_flow_trigger_target(
    client,
    flow,
    accepts_event=accept_correlated_envelope_event,
    resolve_flow_id=resolve_flow_id,
    map_to_flow_input=map_to_flow_input,
    logger=logger,
)
return new_project_envelope_event_received_endpoint_runner(
    project,
    connection,
    [ProjectEnvelopeEventReceivedTriggerRoute(ENVELOPE_OUTCOME_TRIGGER_BINDING, target)],
    logger=logger,
)
```

`new_envelope_event_received_trigger` builds one binding's runner on
`envelope_event_received_webhook_endpoint(connection)` for an application that
wraps its own target. See
[`examples/envelope_outcome`](examples/envelope_outcome/README.md) for the
runnable Worker and Connect server.

## Runnable example

[`examples/envelope_status/flow.py`](examples/envelope_status/flow.py) reads
an envelope with the static `docusign-esignature` connection. It persists only
the request and safe envelope summary. Its optional `notFound`,
`providerRejected`, `invalidResponse`, and `defect` branches are intentionally
unwired, so Dex fails the Flow if one is selected.

From `connectors/docusign/python`, install dependencies and produce a strict
FDG 2.0 file for Dex Web:

```sh
uv sync --locked
dexcli visualize examples/envelope_status/flow.py --language python \
  --schema-version 2.0 --lint app --json --out build/envelope-status \
  --python .venv/bin/python
```

Start `dexcli dev` with that rendering directory. In Dex Web **Connections**,
create `docusign / docusign-esignature`, choose Production account or Developer
account, and follow the authorization guide in the connector manifest. Register
Dex Web's exact redirect URI in the DocuSign app. The OAuth scopes are
`signature` and `extended`; Dex handles access and refresh tokens. The account
ID may stay blank to use the authorizing user's default account. A Connect HMAC
key is optional for this read-only example. Use the owner-only
`connections.json` exported by Dex Web as `DEX_CONNECTOR_CONFIG_FILE`.

Run the Worker and start a Flow with the included sample input:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40901 \
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
uv run python -m examples.envelope_status.main

DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40901 \
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
uv run python -m examples.envelope_status.start \
  docusign-envelope-001 examples/envelope_status/sample-input.json
```

The Worker binding in [`main.py`](examples/envelope_status/main.py) is the
application entrypoint:

```python
project = await load_from_environment()
connection = new_project_connection(project, CONNECTION_NAME)
try:
    await run_worker(EnvelopeStatusFlow(connection))
finally:
    await connection.aclose()
    await project.close()
```

Run the real Dex integration tests against a local `dexcli dev` server:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40901 \
uv run pytest examples/envelope_status/integration_test.py \
  examples/envelope_outcome/integration_test.py
```

The test binds its HTTP provider stand-in to an OS-assigned free port. It checks
retry after a 429 and failure on an unwired `notFound` branch through a real Dex
Worker and Client. The 52 Go-recorded conformance scenarios exercise every
operation's provider requests and exact SDK-encoded outputs. OAuth refresh
agreement has its own generated Go and Python suites. The Trigger unit tests
port the Go and TypeScript Connect endpoint, decoding, and signature tests and
replay a Go-recorded inbox event.

Live DocuSign authorization, account selection, regional host resolution,
provider error codes, envelope read-back timing, PDF downloads, and Connect
configuration have not been verified with a real DocuSign account.
