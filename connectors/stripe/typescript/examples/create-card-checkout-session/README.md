# Card Checkout Session example

This example starts one hosted Stripe card Checkout Session. `CardCheckoutFlow` owns a static `stripe-payments` connection, a request Attribute, the connector's `CreateCardCheckout` Step, and a completion Step that retains the safe Session summary.

Use Node.js 22 or 24. Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web `connections.json` file. That file must select the `stripe` connector as `stripe-payments` with its API key stored as a private credential. Dex CLI/Web R4 support is needed for automatic TypeScript connector discovery; until then, configure this connection through the private project configuration path.

From `connectors/stripe/typescript`, run the Worker:

```sh
npm run example:card-checkout
```

In another terminal using the same configuration and Dex address, start the Flow:

```sh
npm run example:card-checkout:start -- examples/create-card-checkout-session/input.example.json
```

The starter prints the safe Checkout Session summary. Replace the example email and redirect URLs for a real Stripe test account. The only connected provider call is in the connector Step Execute; no credential enters the Flow input, Attributes, or output.

Run the real Dex test against a local Stripe HTTP fixture with `DEX_FLOW_SERVICE_ADDRESS` set:

```sh
npm run test:integration
```

The test verifies the same idempotency key across a lost response and Stripe's key-in-use conflict, one provider object, successful completion, and failure of an unwired `providerRejected` branch. It does not make a live Stripe call.

Generate the strict Flow Definition Graph 2.0 output with Dex CLI 1.28.0 or newer. From `connectors/stripe/typescript`, install this local package into the example so the CLI can read its generated `dex-connector.json`:

```sh
npm run build
npm install --prefix examples/create-card-checkout-session --no-save --no-package-lock --ignore-scripts .
cd examples/create-card-checkout-session
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Dex CLI 1.28.0 recognizes the connector Step and its `created` branch, but reports `unknown_step_target` for the valid `goTo(new StepRef('CreateCardCheckout'), ...)` transition. Strict FDG validation for this routing needs Dex CLI 1.28.1 ([tracking issue #362](https://github.com/superdurable/dex-connectors-library/issues/362)); the real Dex integration above passes.
