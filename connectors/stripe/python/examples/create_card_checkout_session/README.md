# Stripe card Checkout Flow for Python

This runnable Flow creates one hosted Stripe card Checkout Session. Its Worker
uses the static `stripe-payments` connection and resolves credentials from the
Dex project at provider call time.

From `connectors/stripe`, generate the Go connector's strict FDG 2.0 setup
reference while the Python connector descriptor generator is pending:

```sh
mkdir -p build
dexcli visualize ./examples/create-card-checkout-session/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/create-card-checkout-session
```

Start `dexcli dev -flow-rendering-dir ./build/create-card-checkout-session`,
then use Dex Web **Connections** to configure `stripe / stripe-payments`.
Select the API-key-only authorization method and enter the key there. Set
`DEX_CONNECTOR_CONFIG_FILE` to the owner-only `connections.json` produced by
that Dex Web project. The Python Worker consumes its ordinary settings and
private credential without putting the key in Flow state.

From `connectors/stripe/python`, install the package and run the Worker:

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.create_card_checkout_session.main
```

The entrypoint uses this exact application code from [`main.py`](main.py):

```python
project = await load_from_environment()
connection = new_project_connection(project, CONNECTION_NAME)
try:
    await run_worker(CardCheckoutFlow(connection))
finally:
    await connection.aclose()
    await project.close()
```

Start `StripeCreateCardCheckoutSessionPython` from the included Client
entrypoint. Change the Flow ID for each run:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.create_card_checkout_session.start \
  stripe-card-001 examples/create_card_checkout_session/sample-input.json
```

[`sample-input.json`](sample-input.json) contains:

```json
{
  "clientReferenceId": "registration-42",
  "customerEmail": "person@example.com",
  "successUrl": "https://example.com/success",
  "cancelUrl": "https://example.com/cancel",
  "currency": "usd",
  "unitAmount": 7500,
  "productName": "BBQ ticket"
}
```

Only the `created` branch is wired. A definite provider rejection, an
unconfirmed write, an invalid provider response, and a local defect fail the
Flow. Retryable failures use Dex's Step retry policy and keep the same Stripe
`Idempotency-Key`.

With `dexcli dev` running, exercise the example against the real Dex Worker
and Client with a credential-safe fake provider:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  uv run pytest examples/create_card_checkout_session/integration_test.py
```

Dex CLI and Dex Web 1.28.0 can analyze Python connector Flows once the
generated `dex-connector.json` descriptor is checked in. Until then, the Go
FDG above supplies shared Stripe connection setup and `start.py` starts the
Python Flow.
