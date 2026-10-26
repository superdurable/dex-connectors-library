# Amazon SQS queue-consumer example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Amazon SQS operation:

1. `RecordQueueConsumerRequest` records the requested queue;
2. `ReceiveOrderEvent` calls `sqs.NewReceiveMessagesStep` for one message,
   long-polling for the connection's `receiveWaitSeconds` and hiding the
   message for 300 seconds while the Flow processes it;
3. `InspectOrderEvent` parses a received body as an order event such as
   `{"orderId":"1042","event":"paid"}`;
4. `AcknowledgeOrderEvent` calls `sqs.NewDeleteMessageStep` for a valid event,
   and `RecordAcknowledgedOrderEvent` completes as `processed`;
5. `ReleaseOrderEvent` calls `sqs.NewChangeMessageVisibilityStep` with a zero
   timeout for an invalid body, and `RecordReleasedOrderEvent` completes as
   `rejected`, so the queue's redrive policy can move the message to a
   dead-letter queue after its maximum receives.

Only `received`, `deleted`, and `changed` are wired. An empty poll or invalid
receipt handle fails the Flow with the connector's safe reason; SQS may deliver
the message again. Rejected, invalid, and defect outcomes fail without acknowledging, and
the message reappears when its visibility timeout ends.

## Configure

Follow the [Amazon SQS Connector setup](../../README.md), then configure the
`amazon-sqs / amazon-sqs-orders` connection in Dex Web **Connections**. Set
`defaultQueueUrl` to the orders queue, or name `queueUrl` in the Start Flow
input. The example's Steps have no configuration units.

## Run

Generate strict FDG 2.0 from `connectors/amazon/sqs` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/queue-consumer/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/amazon-sqs-queue-consumer
```

Run `dexcli dev` with that build directory. Dex Web or Superverse Studio writes
the connection to the project configuration; run the Worker with the
`DEX_PROJECT_*` environment that names it, as
[project configuration loading](../../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
go run ./examples/queue-consumer
```

The default Worker address is `127.0.0.1:8848`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Send an order event to the queue, for example from the SQS console's
**Send and receive messages** page, then start `AmazonSQSQueueConsumer` with a
unique Flow ID and an empty input `{}`, or `{"queueUrl": "<queue URL>"}`. The
output reports `status`, the message ID and `receiveCount`, the parsed
`orderEvent` or `rejectionReason`, and `isAcknowledgementConfirmed`.

```bash
GOWORK=off go test -race ./examples/queue-consumer/...
GOWORK=off go test -tags=integration ./examples/queue-consumer/... -count=1 -v
```

The integration tests run the Flow on a real Dex Server against the
SigV4-verifying fake in `internal/sqsfake`: a processed and acknowledged event,
a rejected message that returns with `receiveCount` 2, an empty queue that
fails the Flow, and an
unwired rejection that acknowledges nothing. One test delays the deletion's
response by nine seconds, past the async local phase, so Dex dispatches the
acknowledgement again; both attempts send the same handle and the Flow
completes `processed`. Another sends the message nine seconds into a 20-second
long poll and sees one `ReceiveMessage` request, because `receiveMessages` is
sync. The live test in `flow/workflow_live_test.go` runs the same Flow against
a real SQS API; see the connector README for its variables.
