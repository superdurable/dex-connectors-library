# Amazon SQS TypeScript connector

`@superdurable/connector-amazon-sqs` implements `receiveMessages`, `deleteMessage`, and `changeMessageVisibility` as Dex Mutations. The package includes generated operation models, Step factories, mock cases, and `dex-connector.json`; hand-written code signs the SQS JSON protocol with AWS Signature Version 4 and classifies provider outcomes. It has no Trigger.

Install the published package with `npm install @superdurable/connector-amazon-sqs@0.1.0`. Node.js 22 or 24 is required by the Connector SDK. For this checkout, run `npm ci` from this directory.

## Connection

Create an IAM access key in [AWS IAM](https://console.aws.amazon.com/iam) under **Users > your Dex user > Security credentials > Access keys > Create access key**. Restrict the user to `sqs:ReceiveMessage`, `sqs:DeleteMessage`, and `sqs:ChangeMessageVisibility` on the queue ARN. A customer managed KMS key also needs `kms:Decrypt`. Store the access key ID in `access_key_id` and its secret in `secret_access_key` in Dex Web **Connections**. Temporary credentials also need `session_token`; the connector does not renew them. Credentials are resolved before each operation and never placed in Flow input or Attributes.

Set `region` to the Region of the queue URL shown in [SQS Queues](https://console.aws.amazon.com/sqs/v3/home). Set `defaultQueueUrl` to let operation inputs omit `queueUrl`. Leave `endpoint` blank for the Amazon Regional endpoint. For ElasticMQ or LocalStack, set their own loopback endpoint and Region. The [runnable queue consumer](examples/queue-consumer/README.md) binds the static `amazon-sqs-orders` connection:

```ts
const project = await loadFromEnvironment();
const flow = new QueueConsumerFlow(newProjectConnection(project, ConnectionName));
```

This snippet comes from [main.ts](examples/queue-consumer/main.ts). The complete Flow in [flow.ts](examples/queue-consumer/flow.ts) receives one event, deletes a valid order event, and releases an invalid event with a zero visibility timeout. The queue should have a redrive policy and a visibility timeout longer than the Flow's processing time.

## Operation behavior

`receiveMessages` requests at most ten messages. An omitted `waitTimeSeconds` uses the connection's long-poll default; zero returns immediately. An omitted `visibilityTimeoutSeconds` uses the queue's visibility setting. A FIFO queue sends the stable Dex Call ID as `ReceiveRequestAttemptId`, so a retry within SQS's deduplication window gets the same batch. Standard queues may return more messages after a lost response; these reappear when their visibility timeout ends. Received bodies are checked against the SQS MD5 digest. The `received` branch carries at least one message, `empty` carries none, and `providerRejected`, `invalidResponse`, and `defect` are optional.

`deleteMessage` acknowledges a receipt handle. `changeMessageVisibility` sets its timeout from now, with zero releasing the message. Both accept a `messageId` for Results and receipts but do not send it to SQS. Their happy branches are `deleted` and `changed`; `receiptHandleInvalid`, `providerRejected`, and `defect` are optional. A repeated deletion is safe, though a compatible server may reject a handle already deleted. A repeated visibility change starts the timeout again. Transport, 429, and 5xx failures Retry through Dex; exhausted retries fail the Flow. None of these operations declares uncertainty.

## Verification

From the repository root, run `GOWORK=off go run ./cmd/connectorctl generate --check connectors/amazon/sqs/connector.yaml` and `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/amazon/sqs`. From this directory run `npm run check`. The 32 Go-recorded scenarios cover every branch, 429/503, before-send and after-send transport failures, FIFO deduplication, signed requests, and retry stability. With `dexcli dev` running, `npm run integration:queue-consumer` exercises the checked-in Flow through a real Dex Worker and Client. Provider replay and the scripted Dex integration test do not verify a live Amazon SQS account; live provider behavior remains unverified for this TypeScript package.
