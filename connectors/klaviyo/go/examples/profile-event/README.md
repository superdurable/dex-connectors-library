# Klaviyo profile event example

This runnable Flow submits one profile event from Dex Web Start Flow. The `accepted` branch completes after Klaviyo queues the event; optional branches remain unwired and fail the Flow. The connector derives a stable `unique_id` from the Dex call. `ConnectionName` is statically `klaviyo`. Dex Web summary and display views show the completed provider outcome.

## Generate strict FDG 2.0

From `connectors/klaviyo` with the latest stable Dex CLI:

```bash
mkdir -p build
dexcli visualize ./examples/profile-event/flow/workflow.go --schema-version 2.0 --json --out ./build/profile-event
```

The result must report `valid: true`. Local source may report `connector_release_required` until published.

## Run

From the repository root, create a release override and run Dex:

```bash
mkdir -p /tmp/klaviyo-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/klaviyo/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/klaviyo/go \
  --version v0.2.0 --tag connectors/klaviyo/go/v0.2.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/klaviyo-release/connector-release.json \
  --digest-output /tmp/klaviyo-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/klaviyo/go/build" --connector-release-override klaviyo=/tmp/klaviyo-release
```

Set the project environment described by [`projectconfig`](../../../../../sdkgo/projectconfig/README.md), then start the Worker:

```bash
cd connectors/klaviyo/go
go run ./examples/profile-event
```

Configure `klaviyo / klaviyo` in Dex Web Connections using a private key with `events:write` and `profiles:read` from [Klaviyo API keys](https://www.klaviyo.com/settings/account/api-keys). In Start Flow choose `KlaviyoProfileEvent`, provide a unique Flow ID, and enter:

```json
{"email":"member@your-domain.com","metricName":"Signed Up"}
```

Use a real address from an account you control. Klaviyo can return 202 and later discard events for test domains.

## Tests

`GOWORK=off go test ./...` checks deterministic mapping. CI runs the generated-mock real-Dex test. Live Klaviyo delivery remains unverified.
