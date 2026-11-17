# Go topic listing Flow

This runnable Worker registers a `listTopics` Flow and a `publishMessage` Flow through the static `amazon-sns` connection. Each stores its result in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/amazon/sns/go`, run:

```bash
dexcli visualize ./examples/topic-listing/flow/workflow.go --schema-version 2.0 --json --out /tmp/amazon-sns-topic-listing-fdg
dexcli visualize ./examples/topic-listing/flow/publish_message_flow.go --schema-version 2.0 --lint app --json --out /tmp/amazon-sns-publish-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override amazon-sns=<artifact-directory>`. In Dex Web Connections select `amazon-sns`. Set `region` and secret `access_key_id` and `secret_access_key` as described in the [connector README](../../../README.md). Set `session_token` for temporary credentials. Leave `endpoint`, `timeout`, and `maxResponseBytes` blank for the documented behavior and displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/amazon/sns/go`:

```bash
GOWORK=off go run ./examples/topic-listing
```

Start `AmazonSNSTopicListing` with a unique Flow ID and input `{"nextToken":""}`. Start another Flow with the returned token to read the next page. Start `AmazonSNSPublishMessage` with `{"topicARN":"arn:aws:sns:us-east-1:123456789012:example-topic","message":"Report ready","subject":"Operations"}`, replacing the ARN with your selected topic's ARN. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
