# Stripe connector for Python

`dex-connector-stripe` provides five async Stripe operations: hosted card and
ACH Checkout Session creation, Checkout Session and PaymentIntent reads, and
refund creation. Install the released package with:

```sh
pip install dex-connector-stripe==0.1.0
```

Use the `dex_connectors.stripe` import package. The connector requires Python
3.11 or newer, Dex Python SDK 1.28.0 or newer, and the released
`dex-connectors-sdk` version pinned in `pyproject.toml`.

## Configure a connection

The example uses the static connection name `stripe-payments`. In Stripe
Dashboard, choose Test or Live mode, then open **Developers > API keys** at
<https://dashboard.stripe.com/apikeys>. Create a restricted key with Checkout
Session create/read, PaymentIntent and Charge read, and Refund read/write
permissions for the operations you use. The secret begins with `rk_test_` or
`rk_live_`; a standard secret key begins with `sk_test_` or `sk_live_`.
Store it as `secret_key` using the **API key only** connection method. The
secret is resolved at call time and stays out of Flow input, Attributes,
results, receipts, and logs. The optional **API key and webhook** method also
requires a `whsec_` endpoint signing secret from **Developers > Webhooks >
select endpoint > Signing secret > Reveal**. The key and endpoint must use the
same Stripe mode. This Python package currently implements API operations;
the Go connector documents webhook delivery.

The default API endpoint is Stripe's public `/v1` endpoint. Set `endpoint`
only for an explicitly supported compatible gateway. `max_response_bytes`
defaults to one MiB; increase it only when valid complete provider responses
exceed that safety bound. Both values are connection configuration, never
secrets.

The package includes a generated `dex-connector.json` descriptor for Dex CLI
and Dex Web 1.28.0. The Python example uses the Go connector's strict FDG 2.0
graph for Dex Web Connections setup. Strict FDG analysis of its local-to-connector
route requires Dex CLI 1.28.1, which tracks the literal `dex.StepRef` analyzer fix
in issue #362 (dex#1086). Load the project with the Connector SDK's
`projectconfig.load_from_environment`, then call
`stripe.new_project_connection(project, "stripe-payments")`. It validates the
ordinary connection settings and resolves current private credentials for
each operation through `LoadedProject.resolve_credential`. Local Dex Web
credentials are re-read on every call. Do not put the Stripe key in a Flow
start input.

## Operation results

Checkout mutations return `created`, `providerRejected`, `uncertain`,
`invalidResponse`, or `defect`. Queries return `found`, `notFound`,
`providerRejected`, `invalidResponse`, or `defect`. Refund creation returns
`refunded`, `providerRejected`, `uncertain`, `invalidResponse`, or `defect`.
Transient failures request a Dex retry. All writes send the stable Call ID as
Stripe's `Idempotency-Key`. Refunds also write it to
`metadata[dex_call_id]`, then search at most three refund pages after an
unconfirmed server response. Reconcile an `uncertain` outcome before making a
new request with a new key. Keep any application retry override at most 23
hours because Stripe retains idempotency keys for at least 24 hours.

`stripemock.Mock` exposes all five operations with the happy path scenario
from `connector.yaml` as the default. Its named scenario functions cover
every manifest mock branch. Pass `Mock("stripe-payments").connection()` to the
operation-specific Step factories to run a Flow with scripted answers. Provider
unit tests use `httpx.MockTransport` and do not require Stripe credentials.

## Runnable Flow

[`examples/create_card_checkout_session`](examples/create_card_checkout_session/README.md)
uses `new_create_card_checkout_session_step` with the static
`stripe-payments` connection, a `CardCheckoutInput` start value, and a real
Dex `AsyncWorker`. Its [`main.py`](examples/create_card_checkout_session/main.py)
binds the project and Worker with:

```python
project = await load_from_environment()
connection = new_project_connection(project, CONNECTION_NAME)
try:
    await run_worker(CardCheckoutFlow(connection))
finally:
    await connection.aclose()
    await project.close()
```

For Dex FDG 2.0 source analysis, pass an inline `annotations` mapping with
`groupId`, `groupLabel`, and `explanation` to each Step factory. Pass literal
Flow type and `connection_name` strings at their call sites. Factories also
accept `StepAnnotations` for programmatic registration.

The example integration test runs the Flow through a real Dex Worker and
Client with a credential-safe fake provider. A live Stripe account has not
been used to verify API-key authorization, card or ACH Checkout creation,
Checkout Session or PaymentIntent reads, refund creation, or refund
read-back reconciliation.
