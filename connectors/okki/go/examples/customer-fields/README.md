# OKKI Customer Fields Flow

This runnable Worker registers `OKKICustomerFields` with static ConnectionName `okki`. It executes the provider Query once, stores the bounded field dictionary in an Attribute, and completes with the same typed result. Only the happy-path branch is wired.

From `connectors/okki/go`, generate and inspect the strict FDG 2.0 graph:

```bash
dexcli visualize ./examples/customer-fields/flow/workflow.go --schema-version 2.0 --json --out /tmp/okki-customer-fields-fdg
```

Build a connector release artifact, then start `dexcli dev` with `--connector-release-override okki=<artifact-directory>`. In Dex Web Connections select `okki`, follow the [connection guide](../../../README.md), and enter the client ID and protected client secret for the intended CRM company. Leave the access token and other settings blank for displayed defaults. Run the Worker:

```bash
GOWORK=off go run ./examples/customer-fields
```

Start `OKKICustomerFields` with a unique Flow ID and `{}`. The integration test uses a real Dex Worker and Client with the generated provider mock. Live OKKI authorization and field data remain unverified.
