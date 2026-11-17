# Close lookup example

This Worker registers `CloseLookup` with static connection name `close`. It calls `listLeads`, `getLead`, Go `createContact`, Go `listContacts`, Go `listOpportunities`, and Go `listTasks` in one typed Flow. Each real run creates a new contact named `Ada Example` under the first lead.

## Generate and run

From `connectors/close/go`, generate strict FDG 2.0 from the checked-in Flow source:

```bash
mkdir -p /tmp/close-lookup-fdg
dexcli visualize ./examples/close-lookup/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/close-lookup-fdg
```

The command must report `valid: true`. A local module may also report `connector_release_required` until the first release. From the repository root, create local release metadata for Dex Web Connections:

```bash
mkdir -p /tmp/close-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/close/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/close/go \
  --version v0.3.0 --tag connectors/close/go/v0.3.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/close-release/connector-release.json \
  --digest-output /tmp/close-release/connector-release.json.sha256
dexcli dev -open=false -dex-port 9230 -web-port 9231 \
  --flow-rendering-dir /tmp/close-lookup-fdg \
  --connector-release-override close=/tmp/close-release
```

Configure the static `close` connection in Dex Web, then in a second terminal run `GOWORK=off go run ./examples/close-lookup` from `connectors/close` with the project environment described in [`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md). Start `CloseLookup` through Dex Web's Start Flow form.

The deterministic mapping unit test runs with `GOWORK=off go test ./examples/close-lookup/...`. The mock-backed real Dex integration test exercises the contact creation Step and subsequent read without a provider credential. Live provider creation remains unverified.
