# Braintree transaction lookup Flow

This runnable Flow reads one Braintree transaction with `getTransaction` through the static `braintree` connection and persists its profile. Configure `publicKey` and secret `privateKey` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/braintree/go`, run:

```bash
dexcli visualize ./examples/transaction-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/braintree-transaction-read-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override braintree=<artifact-directory>`. In Dex Web Connections select the `braintree` connection. Follow the [connection guide](../../../README.md) to enter `publicKey` and secret `privateKey`; leave `baseUrl`, `apiVersion`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/braintree/go`:

```bash
GOWORK=off go run ./examples/transaction-read
```

Start `BraintreeTransactionRead` with a unique Flow ID and input `{"transactionId":"txn-1"}`. Replace the sample IDs with GraphQL transaction ID from your Braintree account. The Worker connects to Dex Server without an inbound listener.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Braintree authorization and lookup remain unverified.
# Customer read

The same worker registers `BraintreeCustomerRead`. Start it with a `GetCustomerInput` containing a GraphQL `customerId` from a Braintree API result. Its `CallGetCustomer` Step reads the customer and completes with a `CustomerProfile`. It uses the static `braintree` Dex Web connection.

# Customer creation

The worker registers `BraintreeCustomerCreate`. Start it with `CreateCustomerInput{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com"}`. Its `CallCreateCustomer` Step bounds possible duplicate sends and completes with the new `CustomerProfile`. From the connector directory, validate the FDG with `dexcli visualize go/examples/transaction-read/flow/customer_create.go --schema-version 2.0 --lint app --json --out /tmp/braintree-customer-create-fdg --open=false`. With local Dex running and `DEX_FLOW_SERVICE_ADDRESS` set, run `GOWORK=off go test -tags=integration ./examples/transaction-read/flow -run '^TestCustomerCreateUsesDefaultMockWithRealDex$'`.

# Payment method vaulting

The worker registers `BraintreePaymentMethodVault`. Start it with `VaultPaymentMethodInput{PaymentMethodID: "single-use-1", CustomerID: "customer-1"}`, substituting a client-tokenized single-use ID and optional GraphQL customer ID. Its `CallVaultPaymentMethod` Step bounds possible repeated vault or verification sends and completes with the multi-use `PaymentMethodProfile`. Validate the FDG with `dexcli visualize go/examples/transaction-read/flow/payment_method_vault.go --schema-version 2.0 --lint app --json --out /tmp/braintree-payment-vault-fdg --open=false` from the connector directory. With local Dex running and `DEX_FLOW_SERVICE_ADDRESS` set, run `GOWORK=off go test -tags=integration ./examples/transaction-read/flow -run '^TestPaymentMethodVaultUsesDefaultMockWithRealDex$'`.

# Transaction charge

The worker registers `BraintreeTransactionCharge`. Start it with `ChargePaymentMethodInput{PaymentMethodID: "payment-method-1", Amount: "10.00"}`, replacing the sample ID and amount with a tokenized payment method and intended charge. Its `CallChargePaymentMethod` Step uses Braintree's stable `apiRequestKey` and completes with the transaction ID and status. Validate the FDG with `dexcli visualize go/examples/transaction-read/flow/transaction_charge.go --schema-version 2.0 --lint app --json --out /tmp/braintree-transaction-charge-fdg --open=false` from the connector directory. With local Dex running and `DEX_FLOW_SERVICE_ADDRESS` set, run `GOWORK=off go test -tags=integration ./examples/transaction-read/flow -run '^TestTransactionChargeUsesDefaultMockWithRealDex$'`.

# Dispute acceptance

The worker registers `BraintreeDisputeAccept`. Start it with `GetDisputeInput{DisputeID: "dispute-1"}`, replacing the sample with a GraphQL ID for an open case. Its `CallAcceptDispute` Step checks an earlier uncertain send before any resend and completes with the accepted dispute. Acceptance can return funds to the customer. Validate the FDG with `dexcli visualize go/examples/transaction-read/flow/dispute_accept.go --schema-version 2.0 --lint app --json --out /tmp/braintree-dispute-accept-fdg --open=false` from the connector directory. With local Dex running and `DEX_FLOW_SERVICE_ADDRESS` set, run `GOWORK=off go test -tags=integration ./examples/transaction-read/flow -run '^TestDisputeAcceptUsesDefaultMockWithRealDex$'`.
