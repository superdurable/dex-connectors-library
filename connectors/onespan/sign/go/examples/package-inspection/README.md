# OneSpan Sign package inspection Flow

This runnable Flow calls `getPackage` through the static `onespan-sign` connection. It persists the selected package status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/onespan/sign/go`, run:

```bash
dexcli visualize ./examples/package-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/onespan-sign-package-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override onespan-sign=<artifact-directory>`. In Dex Web Connections select the `onespan-sign` connection. Follow the [connection guide](../../../README.md) to reveal an account API Key and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for Sandbox defaults; select the correct API root for Production.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/onespan/sign/go`:

```bash
GOWORK=off go run ./examples/package-inspection
```

Start `OneSpanSignPackageInspection` with a unique Flow ID and input `{"packageId":"9sKhW-h-qS9m6Ho3zRv3n2a-rkI="}`. Replace the sample with a package ID returned by OneSpan Sign. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real OneSpan authorization and retrieval remain unverified.
