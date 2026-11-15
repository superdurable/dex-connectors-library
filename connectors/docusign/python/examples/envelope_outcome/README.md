# DocuSign envelope outcome example

This Python example is the smallest consuming application of the
`envelopeEventReceived` Trigger. The `envelope-outcomes` binding starts one
`DocuSignEnvelopeOutcomePython` Flow per correlated Connect outcome, with the
Flow ID `docusign-envelope-outcome-<envelope ID>-<event>`, so a Connect retry or
a second Connect configuration delivering the same outcome finds the same Flow:

1. `accept_correlated_envelope_event` admits only an envelope whose hidden text
   custom field `dexSigningRequestId` holds 1-64 letters, digits, `-`, or `_`,
   as an application that created the envelope with
   `createEnvelopeFromTemplate` sets it. Other outcomes are consumed without a
   Flow.
2. `RecordEnvelopeEvent` validates the delivery and stores it in the
   `docusign-envelope-event` Attribute.
3. `ReadEnvelope` runs `getEnvelope`, so the record holds the envelope's current
   status. Its optional branches are unwired and fail the Flow when selected.
4. `CompleteEnvelopeOutcome` stores the outcome, the signing request ID, and the
   envelope in `docusign-envelope-outcome` and completes the Flow.

The TypeScript [envelope outcome example](../../../typescript/examples/envelope-outcome/README.md)
resumes a waiting signing Flow through an RPC instead, and the Go
[envelope-signing example](../../../go/examples/envelope-signing/README.md) adds
the status-poll fallback, the signing deadline, and the signed document
download.

[`main.py`](main.py) serves the Connect endpoint at `/docusign/connect` and a
readiness check at `/readyz` on `CONNECT_BIND_ADDRESS` (default
`127.0.0.1:8862`) with uvicorn, whose lifespan replays the binding's durable
project inbox and starts receiving. Configure the `docusign-esignature`
connection, including `connect_hmac_key`, and the `envelope-outcomes` binding in
Dex Web as the [connector README](../../../README.md#connect-configuration)
describes; the binding has no configuration units and saves as `{}`, which
records all three outcomes. Expose the endpoint through an HTTPS tunnel as the
Connect configuration's URL to Publish. From `connectors/docusign/python`,
render the Flow and run the application:

```sh
uv sync --locked
dexcli visualize examples/envelope_outcome/flow.py --language python \
  --schema-version 2.0 --lint app --json --out build/envelope-outcome \
  --python .venv/bin/python

DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40901 \
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
uv run --with uvicorn python -m examples.envelope_outcome.main
```

Send an envelope whose custom fields include `dexSigningRequestId`; signing,
declining, or voiding it makes Connect deliver the outcome and starts the Flow.
Set `LOG_LEVEL=debug` to see every delivery.

The [unit tests](../../tests/test_envelope_outcome_example.py) cover the
correlation filter, Flow IDs, and input mapping. The
[integration test](integration_test.py) runs this application against a real
Dex Server with a local DocuSign stand-in: a signed outcome starts one Flow that
reads the envelope back, a redelivery reuses it, and a forged signature, an
uncorrelated outcome, and a recipient event start nothing:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40901 \
uv run pytest examples/envelope_outcome/integration_test.py
```

Live DocuSign Connect deliveries have not been verified.
