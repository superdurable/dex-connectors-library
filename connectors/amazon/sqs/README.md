# Amazon SQS Connector

> **Verification status: partial live.** Verified against ElasticMQ 1.6.14 on a real Dex stack; Amazon SQS itself is not verified. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Amazon SQS Connector consumes messages from Amazon SQS standard and FIFO
queues, and from SQS-compatible servers such as ElasticMQ, through the SQS JSON
protocol. It exposes operation-specific Dex Step factories:

| Operation | Kind | SQS action | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- | --- |
| `receiveMessages` | Mutation | `ReceiveMessage` | sync | `received` | `empty`, `providerRejected`, `invalidResponse`, `defect` |
| `deleteMessage` | Mutation | `DeleteMessage` | async | `deleted` | `receiptHandleInvalid`, `providerRejected`, `defect` |
| `changeMessageVisibility` | Mutation | `ChangeMessageVisibility` | async | `changed` | `receiptHandleInvalid`, `providerRejected`, `defect` |

A consuming Flow receives a batch, processes each message, and then
acknowledges it with `deleteMessage`, or releases it with
`changeMessageVisibility` and a zero timeout. A message the Flow never
acknowledges reappears on the queue when its visibility timeout ends, so a
failed Flow leaves its message for another consumer or for the queue's
dead-letter redrive policy. Acknowledging only after processing makes the
consumer at-least-once: processing must tolerate a message delivered again,
which `ReceiveCount` above 1 reveals. This release sends no messages, has no
Trigger, and has no Studio pickers; see [Not in this release](#not-in-this-release).

## Authentication

A connection holds an access key ID, a secret access key, and, for temporary
AWS credentials only, a session token. All three are stored as secrets. Every
request is signed with AWS Signature Version 4 for the configured `region`;
the secret access key never leaves the Worker, and neither it nor the session
token appears in a request body, a Result, a Failure, a receipt, or a log.
Credentials are reread before every call, so a replaced key takes effect
without a restart.

Create an IAM user only for Dex at <https://console.aws.amazon.com/iam> >
**Users** > **Create user**, attach an inline policy that allows
`sqs:ReceiveMessage`, `sqs:DeleteMessage`, and `sqs:ChangeMessageVisibility`
on the queue's ARN, such as `arn:aws:sqs:eu-west-1:123456789012:orders`, then
open **Security credentials** > **Access keys** > **Create access key**, choose
**Other**, and copy both values from **Retrieve access keys**. A queue
encrypted with a customer managed AWS KMS key also needs `kms:Decrypt` on that
key; SSE-SQS needs nothing more. Temporary `ASIA...` credentials with a session
token expire and are not renewed, so an expired token selects
`providerRejected` until all three values are replaced. IAM roles, instance
profiles, IAM Identity Center (SSO), and AWS shared configuration are out of
scope: Dex Web connections hold static secrets.

Copy the queue URL from <https://console.aws.amazon.com/sqs/v3/home> >
**Queues** > the queue > **Details** > **URL**, and set `region` to the Region
it names. Set `defaultQueueUrl` to let operations leave `queueUrl` blank. For
ElasticMQ or LocalStack, set `endpoint` to the server's address, such as
`http://127.0.0.1:9324`, and `region` to the server's Region.

Name the factory connection and open the same name from the project
configuration at application startup, as
[`examples/queue-consumer/main.go`](examples/queue-consumer/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := sqs.NewProjectConnection(project, queueconsumer.ConnectionName)
```

Dex Web or Superverse Studio writes that configuration, and the application
reads it through the `DEX_PROJECT_*` environment described in
[project configuration loading](../../../sdkgo/projectconfig/README.md#application-loading).

## Endpoint, Region, and queue URLs

A blank `endpoint` uses the Amazon SQS Regional endpoint
`https://sqs.<region>.<partition suffix>`, with the suffixes of the AWS SDK's
partition table. A custom endpoint must use HTTPS, except a loopback HTTP
address for a server on the Worker's own host. Every request is a signed
`POST` of `application/x-amz-json-1.0` to the endpoint with
`X-Amz-Target: AmazonSQS.<Action>`; the queue URL travels in the body, so the
connector signs requests only for the configured endpoint, never for the host a
queue URL names.

A queue URL is `http(s)://host/<owner>/<queue name>`, where the name is 1 to 80
letters, digits, hyphens, and underscores including any `.fifo` suffix; user
information, queries, and fragments are rejected. A name ending in `.fifo` is a
FIFO queue. An Amazon SQS queue URL that names another Region than `region`
selects `defect` before any request, with the Region to set.

## Why the official signer, not the full AWS SDK

As in the [Amazon S3 Connector](../s3/README.md#why-the-official-signer-not-the-full-aws-sdk),
the connector depends only on `github.com/aws/aws-sdk-go-v2` v1.43.0 for its
`aws/signer/v4` package, which is already in the module graph of `sdkgo`. The
full `service/sqs` client would retry each call up to three times on its own,
hiding attempts from Dex, deserialize responses without the connector's size
bounds, and carry provider message text in its errors. Sending the three
JSON-protocol requests directly keeps every response bounded, every retry owned
by Dex, and every Failure free of provider text.

## Operations

Every operation sends one request, bounded by 25 seconds, below the 30-second
Execute timeout. Transport failures, 408, 429, `RequestThrottled`,
`ThrottlingException`, `KmsThrottled`, and 5xx except 501 return Retry,
honoring `Retry-After`. `QueueDoesNotExist`, credential and signature errors,
`AccessDenied`, KMS key errors, `OverLimit`, and other 4xx select
`providerRejected`. Failures name the HTTP status and the SQS error code, such
as `HTTP 400 QueueDoesNotExist`, never the error message text. Receipts carry
the Call ID, the queue URL (with `#<messageId>` when the input names the
message), and the `x-amzn-RequestId` response header.

### receiveMessages

`ReceiveMessagesInput` sets `QueueURL`, `MaxMessages` from 1 to 10 (zero uses
the connection's `maxMessagesPerReceive`), `WaitTimeSeconds` from 0 to 20 (nil
uses the connection's `receiveWaitSeconds`; 0 returns at once), and
`VisibilityTimeoutSeconds` from 0 to 43200 (nil keeps the queue's own
timeout). Each `Message` carries its ID, the receipt handle of this delivery,
its body verified against `MD5OfBody`, `SentAt`, `FirstReceivedAt`,
`ReceiveCount`, the FIFO group, deduplication ID, and sequence number, and
every custom message attribute. `received` holds at least one message; `empty`
means none arrived within the wait. A batch larger than `maxResponseBytes`, a
body whose digest does not match, or an invalid document selects
`invalidResponse`; its messages reappear when their visibility timeout ends,
and the queue's redrive policy counts the delivery.

`receiveMessages` is a Mutation, because a receive hides each message and
raises its receive count. It declares sync durability: a long poll on an idle
queue lasts up to 20 seconds, past Dex's seven-second async local phase, after
which an async Step would be dispatched again and hide further messages. The
example's real-Dex test long-polls for nine seconds and sees exactly one
`ReceiveMessage` request. An application that always passes a short
`WaitTimeSeconds` can override durability to async with `StepOptionsOverride`.

A receive is safe to retry. For a FIFO queue every attempt of one Step
execution sends the Call ID as `ReceiveRequestAttemptId`, so a retry within
five minutes gets the same batch and receipt handles back. A standard queue has
no such ID: messages a lost response received stay hidden until their
visibility timeout ends and are then delivered again, with a higher
`ReceiveCount`. They are delayed, never lost.

### deleteMessage

The acknowledgement. Pass the `ReceiptHandle` of the message's latest delivery;
`MessageID` optionally labels the Result and receipt and is not sent. Each
receive issues a new handle. Amazon SQS documents that a deletion with an
outdated handle "succeeds, but the message will not be deleted", so `deleted`
confirms the request, and a message that a later receive redelivered is
deleted only through its newest handle. `receiptHandleInvalid` covers a
malformed handle, an expired FIFO handle (SQS reports `InvalidParameterValue`
about the receipt handle, which the connector recognizes without keeping the
message text), and a server that forgets the handle of a deleted message.

Deletion is safe to repeat, so a timeout or lost response returns Retry and
`deleteMessage` declares no `uncertain` branch. Against ElasticMQ, a repeat
after a successful deletion selects `receiptHandleInvalid` while the message
stays deleted; the live suite records this. Whether Amazon SQS answers such a
repeat with success is not verified live. Applications that need to tell the
two apart treat `receiptHandleInvalid` as "the message may be delivered again"
and keep processing idempotent, as the example does.

### changeMessageVisibility

Sets the message's visibility timeout to `VisibilityTimeoutSeconds` from now,
0 to 43200. Zero releases the message to other consumers at once, which is how
a Flow rejects a message it cannot process so that the redrive policy can move
it to a dead-letter queue after its maximum receives; a larger value extends a
long processing step. `VisibleAfter` is measured from the Worker clock. A
repeated attempt restarts the same timeout from the later attempt.
`receiptHandleInvalid` covers `MessageNotInflight`, an unknown handle, and an
expired FIFO handle.

### Receipt handles in Flow state

A receipt handle authorizes only deleting or re-timing one delivery of one
message, and only while that delivery is in flight. It is not a credential, and
the Flow must carry it from `receiveMessages` to the acknowledgement, so it is
part of the Result. The connection's secret access key and session token never
are: a response that contains either selects `invalidResponse` instead of
entering Flow state.

## Not in this release

- **Trigger**: a queue Trigger that starts one Flow per message needs a source
  that polls, inboxes, and extends visibility while the Flow runs, and a choice
  of whether the source or the Flow acknowledges. This release lets the Flow
  own receiving and acknowledging through Steps.
- **Sending**: `SendMessage` and batch sends.
- **Batch acknowledgement**: `DeleteMessageBatch` and
  `ChangeMessageVisibilityBatch`, whose partial failures need per-entry
  branches.
- **Pickers**: Studio commands send only HTTPS `GET`, and SQS's `ListQueues`
  requires a SigV4-signed `POST`, so queue URLs are guided input or
  `defaultQueueUrl`.
- Verifying `MD5OfMessageAttributes`, queue management, and IAM roles.

## Example

[`examples/queue-consumer`](examples/queue-consumer) is a runnable Dex Web
**Start Flow** example that receives one order event, acknowledges it with
`deleteMessage` when it is valid, and releases it with
`changeMessageVisibility` when it is not.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example's real-Dex tests
cover every business outcome, a duplicate acknowledgement dispatch, and a long
poll past the async phase against the SigV4-verifying fake in
`internal/sqsfake` (two tests take about nine seconds each):

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

The live suite runs the connector and the example against a real SQS API. It
creates a disposable queue per test and deletes it afterwards. For a local
ElasticMQ server, use a secret of realistic length, because the connector
rejects any response that repeats the secret:

```bash
docker run --rm -p 127.0.0.1:9324:9324 softwaremill/elasticmq-native:1.6.14
SQS_CONNECTOR_TEST_ENDPOINT=http://127.0.0.1:9324 SQS_CONNECTOR_TEST_REGION=elasticmq \
SQS_CONNECTOR_TEST_ACCESS_KEY_ID=sqsconnectorlive SQS_CONNECTOR_TEST_SECRET_ACCESS_KEY=<a long test secret> \
  GOWORK=off go test -tags=live ./... -count=1 -v
```

For Amazon SQS, leave `SQS_CONNECTOR_TEST_ENDPOINT` blank, set the Region, and
use a key whose policy also allows `sqs:CreateQueue`, `sqs:SendMessage`, and
`sqs:DeleteQueue` on `arn:aws:sqs:<region>:<account>:dex-sqs-*`.
