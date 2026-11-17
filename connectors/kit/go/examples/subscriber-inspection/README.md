# Kit subscriber-inspection Flow

This runnable Worker registers `KitSubscriberInspection`, `KitTagsInspection`, and `KitCustomFieldsInspection` with the static `kit` connection. Their subscriber, tag, and custom-field pages are saved in Attributes and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. The API Key does not enter Flow results.

## Strict FDG 2.0 generation

From `connectors/kit/go`, run:

```bash
dexcli visualize ./examples/subscriber-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/kit-subscriber-inspection-fdg
dexcli visualize ./examples/subscriber-inspection/flow/tags_workflow.go --schema-version 2.0 --lint app --json --out /tmp/kit-tags-fdg
dexcli visualize ./examples/subscriber-inspection/flow/custom_fields_workflow.go --schema-version 2.0 --lint app --json --out /tmp/kit-custom-fields-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override kit=<artifact-directory>`. In Dex Web Connections select `kit / kit`. Open https://app.kit.com/account_settings/developer_settings > Settings > Developer > V4 API Keys, add a new key, and copy it into the secret `api_key` field. Leave `baseUrl`, timeout, and response-size fields blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/kit/go`:

```bash
GOWORK=off go run ./examples/subscriber-inspection
```

Start `KitSubscriberInspection`, `KitTagsInspection`, or `KitCustomFieldsInspection` with a unique Flow ID and input `{"perPage":20}`. For another page, pass the returned `nextCursor` as `after`. The Worker connects to Dex Server without an inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/subscriber-inspection/flow -count=1`. Live provider authorization remains a separate check.
