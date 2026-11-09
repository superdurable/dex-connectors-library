# GetResponse contact-inspection Flow

This runnable Flow calls `listContacts` with the static `getresponse` connection. Its happy-path contact page is saved in an Attribute and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. Only basic contact fields enter the Flow result; the API Key does not.

## Strict FDG 2.0 generation

From `connectors/getresponse/go`, run:

```bash
dexcli visualize ./examples/contact-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/getresponse-contact-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override getresponse=<artifact-directory>`. In Dex Web Connections select `getresponse / getresponse`. Sign in at https://app.getresponse.com/api, open Integrations and API > API, and generate or copy the API Key into the secret `api_key` field. This initial release supports SMB accounts; leave `baseUrl`, timeout, and response-size fields blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/getresponse/go`:

```bash
GOWORK=off go run ./examples/contact-inspection
```

Start `GetResponseContactInspection` with a unique Flow ID and input `{"page":1,"perPage":20}`. The Worker connects to Dex Server without an inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/contact-inspection/flow -count=1`. Live provider authorization remains a separate check.
