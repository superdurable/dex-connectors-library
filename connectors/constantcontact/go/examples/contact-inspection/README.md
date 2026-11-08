# Constant Contact contact inspection Flow

This runnable Flow calls `getContact` with the static `constantcontact` connection. Its happy-path result is saved in an Attribute and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. OAuth credentials never enter Flow state.

## Strict FDG 2.0 generation

From `connectors/constantcontact/go`, run:

```bash
dexcli visualize ./examples/contact-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/constantcontact-contact-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override constantcontact=<artifact-directory>`. In Dex Web Connections select the `constantcontact` connection, register the displayed redirect URI on the Constant Contact Developer Portal's My Applications page, enter the API Key and Client Secret, and authorize the `contact_data` and `offline_access` scopes. Leave `baseUrl` blank for the public V3 endpoint.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/constantcontact/go`:

```bash
GOWORK=off go run ./examples/contact-inspection
```

Start `ConstantContactContactInspection` with a unique Flow ID and input `{"contactId":"1898ae62-4752-11e9-9c8a-fa163e6b01c1"}`. Obtain a real UUID through a V3 contacts list; the sample UUID is only for mocks. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
