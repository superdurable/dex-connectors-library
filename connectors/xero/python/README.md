# Xero Python connector

`dex-connector-xero` v0.1.0 exposes all six Xero contact, invoice, and payment
operations through generated Dex Step factories. It requires
`dex-connectors-sdk==0.7.0` and uses generated Custom Connection and OAuth
refresh wiring. Configure `xero-books` in Dex Web **Connections** using the
[connector setup](../README.md#xero-setup). Provider operations use exact
decimal strings and send writes under the Call ID as `Idempotency-Key`.
Wire decimals retain their exact digits, including `-0` and trailing zeros; exponent-form numbers are rejected as invalid responses.

The [runnable contact lookup](examples/contact_lookup/README.md) is the minimal
consuming application. From this directory, install and verify it with:

```sh
uv sync --locked
dexcli visualize examples/contact_lookup/flow.py --schema-version 2.0 \
  --json --python .venv/bin/python --out /tmp/xero-python-contact-lookup
```

The command reports `valid: true`. Start `dexcli dev` on available ports,
set `DEX_FLOW_SERVICE_ADDRESS` and the project configuration exported by Dex
Web, then run `uv run python -m examples.contact_lookup.main`. The Worker
uses the static `xero-books` connection. Start
`XeroFindContactByEmailPython` with an input such as
`{"emailAddress":"accounts@example.com"}` and a unique Flow ID. The
`found` branch completes with the contact; unwired optional branches fail.

Targeted verification:

```sh
uv run pytest tests -q
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:45901 \
  uv run pytest examples/contact_lookup/integration_test.py -q
```

The integration test needs an already running `dexcli dev` at the selected
address and binds its provider stand-in to an OS-assigned free port. The
Go-recorded conformance fixtures and generated refresh agreement also run in
`make test-connector-conformance CONNECTOR_DIRECTORY=connectors/xero` from
the repository root. Live Xero authorization and provider behavior are
unverified for this port.
