# PayPal resource-read example

This Flow uses the static `paypal` connection and `paypal.NewGetOrderStep` to read one provider resource. Start input is typed as `Request`; the result is `Outcome`. Optional error branches are unwired and fail the Flow.

## Generate strict FDG 2.0

From `connectors/paypal`:

```sh
mkdir -p build
dexcli visualize ./examples/resource-read/flow/workflow.go --schema-version 2.0 --json --out ./build/resource-read
```

The command should report `valid: true`. A local unpublished module can produce a release metadata warning.

## Configure and run

From the repository root, build a local release override, then start a Dex stack with the rendered graph:

```sh
mkdir -p /tmp/paypal-release
go run ./cmd/connectorctl release-artifact --manifest connectors/paypal/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/paypal --version v0.1.0 --tag connectors/paypal/v0.1.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/paypal-release/connector-release.json --digest-output /tmp/paypal-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/paypal/build" --connector-release-override paypal=/tmp/paypal-release
```

In Dex Web **Connections**, create the `paypal` connection with the manifest's authorization guide. Then start the Worker in another terminal:

```sh
cd connectors/paypal
go run ./examples/resource-read
```

With the current pull Worker SDK, the Worker connects outbound to Dex; older SDKs listen on `127.0.0.1:8873`. In **Start Flow**, choose `PayPalResourceRead`, the Worker at `127.0.0.1:8873`, a unique Flow ID, and input like:

```json
{"orderID":"ORDER101"}
```

The Flow completes with the provider value. A missing resource selects the unwired `notFound` branch and fails the Flow.

The deterministic unit test checks input mapping and Flow identity. `workflow_mock_integration_test.go` runs the manifest default through a real Dex Worker in CI; it is not run during this host's heavy-work pause.
