# Go topic listing Flow

This runnable Worker registers `listTopics`, `publishMessage`, `listSubscriptionsByTopic`, `getTopicAttributes`, and `notificationReceived` Flows through the static `amazon-sns` connection. Each stores its result in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/amazon/sns/go`, run:

```bash
dexcli visualize ./examples/topic-listing/flow/workflow.go --schema-version 2.0 --json --out /tmp/amazon-sns-topic-listing-fdg
dexcli visualize ./examples/topic-listing/flow/publish_message_flow.go --schema-version 2.0 --lint app --json --out /tmp/amazon-sns-publish-fdg
dexcli visualize ./examples/topic-listing/flow/subscription_listing_flow.go --schema-version 2.0 --lint app --json --out /tmp/amazon-sns-subscriptions-fdg
dexcli visualize ./examples/topic-listing/flow/topic_attributes_flow.go --schema-version 2.0 --lint app --json --out /tmp/amazon-sns-topic-attributes-fdg
dexcli visualize ./examples/topic-listing/flow/notification_flow.go --schema-version 2.0 --lint app --json --out /tmp/amazon-sns-notification-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev -open=false` with `--connector-release-override amazon-sns=<artifact-directory>`. In Dex Web Connections select `amazon-sns`. Set `region` and secret `access_key_id` and `secret_access_key` as described in the [connector README](../../../README.md). Set `session_token` for temporary credentials. Leave `endpoint`, `timeout`, and `maxResponseBytes` blank for the documented behavior and displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/amazon/sns/go`:

```bash
GOWORK=off go run ./examples/topic-listing
```

Start `AmazonSNSTopicListing` with a unique Flow ID and input `{"nextToken":""}`. Start another Flow with the returned token to read the next page. Start `AmazonSNSPublishMessage` with `{"topicARN":"arn:aws:sns:us-east-1:123456789012:example-topic","message":"Report ready","subject":"Operations"}`, replacing the ARN with your selected topic's ARN. The Worker connects to Dex Server without an inbound listener.

Start `AmazonSNSSubscriptionListing` with a unique Flow ID and input `{"topicARN":"arn:aws:sns:us-east-1:123456789012:example-topic"}`, replacing the ARN with your topic's ARN from [SNS Console > Topics](https://console.aws.amazon.com/sns). Pass `nextToken` from the result to list the next page; blank starts at the beginning.

Start `AmazonSNSTopicAttributes` with the same topic ARN to read its current SNS settings.

For notifications, the Trigger binding in Dex Web shows a required **Notification topic** field with the SNS Console > Topics path, ARN format, and Region guidance. Set `SNS_TOPIC_ARN` to that same complete topic ARN and start the Worker. It listens at `127.0.0.1:9132/sns/notifications` by default; `SNS_WEBHOOK_BIND_ADDRESS` changes the local bind address. Expose this route through a public HTTPS reverse proxy, then use `subscribeEndpoint` with protocol `https` and that public URL, or create the subscription in [SNS Console > Subscriptions](https://console.aws.amazon.com/sns). The endpoint verifies Amazon's RSA signature and signing certificate, confirms SNS's signed subscription challenge, and starts `AmazonSNSNotificationIntake` using the provider message ID. No AWS account was available for live confirmation and delivery testing.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
