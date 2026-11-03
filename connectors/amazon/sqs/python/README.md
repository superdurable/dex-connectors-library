# Amazon SQS connector for Python

Install with `pip install dex-connector-amazon-sqs==0.1.0`. The package
requires `dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and
implements the manifest Mutations `receiveMessages`, `deleteMessage`, and
`changeMessageVisibility` over the SQS JSON protocol with the same validation,
branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `receiveMessages` | `new_receive_messages_step` | `received` | `empty`, `providerRejected`, `invalidResponse`, `defect` |
| `deleteMessage` | `new_delete_message_step` | `deleted` | `receiptHandleInvalid`, `providerRejected`, `defect` |
| `changeMessageVisibility` | `new_change_message_visibility_step` | `changed` | `receiptHandleInvalid`, `providerRejected`, `defect` |

Requests are signed with AWS Signature Version 4 for the connection's Region,
producing the same signature as the AWS Go SDK signer. A queue URL on an Amazon
endpoint must be in the signing Region. A FIFO receive sends the Call ID as
`ReceiveRequestAttemptId`, so a retry within five minutes returns the same
batch. Received messages are validated like Go, including the body's MD5
digest; binary attribute values are standard base64 strings.

Throttling, request timeouts, 5xx other than 501, and transport failures
retry, honoring `Retry-After`. An invalid or expired receipt handle selects
`receiptHandleInvalid`; other rejections select `providerRejected` with the
SQS error code, never provider message text.

## Authorization

Create the Dex Web connection described in the
[connector README](../README.md) with an access key ID, secret access key, and
optional session token. `new_project_connection` reads them before every
operation.

## Example

[`examples/queue_consumer`](examples/queue_consumer/README.md) receives one
batch and deletes each message; an empty receive completes the run. Its README
includes strict FDG 2.0 generation, Dex Web setup, and Worker instructions.

## Tests

`sqsmock.Mock` scripts every operation and defaults to the manifest answers.
Run `uv run pytest tests` for Go-recorded provider conformance, SigV4, and
manifest contract checks. The example integration test runs the same Flow on
a real Dex Worker and Client with a local SQS stand-in. Live Amazon SQS
authorization and delivery behavior remain unverified for this Python port.
