# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for SQS signing, receive, delete, and visibility changes."""

import hashlib
import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
)
from dex_connectors.sdk.projectconfig import load_dex_web_project

from dex_connectors.amazon_sqs import (
    CHANGE_MESSAGE_VISIBILITY_DEFINITION,
    CHANGED,
    DEFECT,
    DELETE_MESSAGE_DEFINITION,
    DELETED,
    EMPTY,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    RECEIPT_HANDLE_INVALID,
    RECEIVE_MESSAGES_DEFINITION,
    RECEIVED,
    ChangeMessageVisibilityInput,
    Client,
    Config,
    Credentials,
    DeleteMessageInput,
    MessageAttributeValue,
    ReceiveMessagesInput,
    new_project_connection,
)
from dex_connectors.amazon_sqs.sigv4 import sign_headers

CONNECTION = ConnectionRef("amazon-sqs", "amazon-sqs")
NOW = datetime(2026, 10, 25, 9, 0, tzinfo=UTC)
QUEUE = "https://sqs.eu-west-1.amazonaws.com/123456789012/orders"
FIFO_QUEUE = QUEUE + ".fifo"
SECRET = "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
Responder = Callable[[httpx.Request], httpx.Response]


def credentials(session_token: str = "") -> Credentials:
    return Credentials(SecretString("AKIDEXAMPLE"), SecretString(SECRET), SecretString(session_token))


def make_client(respond: Responder, provider: object | None = None, **config: object) -> Client:
    return Client(
        Config(**{"endpoint": "", "default_queue_url": "", "region": "eu-west-1", **config}),  # type: ignore[arg-type]
        provider or StaticCredentialProvider({CONNECTION: credentials()}),  # type: ignore[arg-type]
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
        now=lambda: NOW,
    )


def answer(*responses: httpx.Response, requests: list[httpx.Request] | None = None) -> Responder:
    remaining = list(responses)

    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return remaining.pop(0) if len(remaining) > 1 else remaining[0]

    return respond


def call_for(definition: object) -> Call:
    call_id = CallID(str(uuid4()))
    return Call(call_id, CONNECTION, definition.operation, IdempotencyKey(str(call_id)))  # type: ignore[attr-defined]


def sqs_message(body: str = '{"orderId":"1042"}', **fields: object) -> dict[str, object]:
    return {
        "MessageId": "order-1042",
        "ReceiptHandle": "receipt-order-1042",
        "MD5OfBody": hashlib.md5(body.encode()).hexdigest(),
        "Body": body,
        "Attributes": {
            "ApproximateReceiveCount": "2",
            "SentTimestamp": "1792918800000",
            "ApproximateFirstReceiveTimestamp": "1792918801500",
        },
        **fields,
    }


GO_SIGNATURES = {
    (
        "https://sqs.eu-west-1.amazonaws.com/",
        "",
    ): "6a95f61a3637891d99a2eeb2c034555ea50b44eea17c4ce4ca36af1eca4aaa6e",
    ("https://sqs.eu-west-1.amazonaws.com/", "FwoGZXIvYXdzEJr//token+="): (
        "535fb897243f4250e678ccd555f1aadec14aeb6dada76d8dc373f4fb227efe1a"
    ),
    (
        "http://127.0.0.1:9324/queue-path",
        "",
    ): "1c7b437d8a53154f53601a24bb8d1fd8e1658ccb75f8a13bc171177bc9eab2a0",
    ("http://127.0.0.1:9324/queue-path", "FwoGZXIvYXdzEJr//token+="): (
        "e2518eed62e30019b584e603e75bc3eb83a17f4c8a6724e69fb926131c06a709"
    ),
}


@pytest.mark.parametrize(("target", "token"), list(GO_SIGNATURES))
def test_sigv4_matches_aws_go_sdk_signer(target: str, token: str) -> None:
    """Signatures computed by aws-sdk-go-v2's v4.Signer for the same request."""
    body = b'{"QueueUrl":"https://sqs.eu-west-1.amazonaws.com/123456789012/orders","MaxNumberOfMessages":10}'
    headers = sign_headers(
        method="POST",
        url=target,
        headers={"Content-Type": "application/x-amz-json-1.0", "X-Amz-Target": "AmazonSQS.ReceiveMessage"},
        body=body,
        access_key_id="AKIDEXAMPLE",
        secret_access_key=SECRET,
        session_token=token,
        region="eu-west-1",
        service="sqs",
        signing_time=NOW,
    )
    assert headers["Authorization"].endswith("Signature=" + GO_SIGNATURES[(target, token)])
    assert headers["X-Amz-Date"] == "20261025T090000Z"


@pytest.mark.parametrize(
    ("explicit_port", "implicit_port"),
    [
        ("https://sqs.example.com:443/", "https://sqs.example.com/"),
        ("http://localhost:80/", "http://localhost/"),
    ],
)
def test_sigv4_uses_the_host_httpx_sends(explicit_port: str, implicit_port: str) -> None:
    def authorization(url: str) -> str:
        return sign_headers(
            method="POST",
            url=url,
            headers={"Content-Type": "application/x-amz-json-1.0"},
            body=b"{}",
            access_key_id="AKIDEXAMPLE",
            secret_access_key=SECRET,
            session_token="",
            region="eu-west-1",
            service="sqs",
            signing_time=NOW,
        )["Authorization"]

    assert httpx.Request("POST", explicit_port).headers["Host"] == httpx.Request(
        "POST", implicit_port
    ).headers["Host"]
    assert authorization(explicit_port) == authorization(implicit_port)


@pytest.mark.asyncio
async def test_receive_sends_signed_json_request_and_validates_messages() -> None:
    requests: list[httpx.Request] = []
    attributes = {
        "kind": {"DataType": "Binary", "BinaryValue": "AP8="},
        "source": {"DataType": "String", "StringValue": "web"},
    }
    body = {"Messages": [sqs_message(MessageAttributes=attributes)]}
    result = await run_mutation(
        make_client(
            answer(httpx.Response(200, json=body, headers={"X-Amzn-Requestid": "req-1"}), requests=requests),
            default_queue_url=QUEUE,
        ).receive_messages(),
        call_for(RECEIVE_MESSAGES_DEFINITION),
        ReceiveMessagesInput(max_messages=5, wait_time_seconds=0, visibility_timeout_seconds=60),
    )
    assert result.branch == RECEIVED and result.receipt.provider_request_id == "req-1"
    message = result.value.messages[0]
    assert message.receive_count == 2 and message.sent_at == datetime(2026, 10, 25, 9, 0, tzinfo=UTC)
    assert message.first_received_at == datetime(2026, 10, 25, 9, 0, 1, 500000, tzinfo=UTC)
    assert message.message_attributes == {
        "kind": MessageAttributeValue("Binary", binary_value="AP8="),
        "source": MessageAttributeValue("String", "web"),
    }
    request = requests[0]
    assert str(request.url) == "https://sqs.eu-west-1.amazonaws.com/"
    assert request.headers["X-Amz-Target"] == "AmazonSQS.ReceiveMessage"
    assert request.headers["Content-Type"] == "application/x-amz-json-1.0"
    assert request.headers["Authorization"].startswith(
        "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20261025/eu-west-1/sqs/aws4_request, "
        "SignedHeaders=content-length;content-type;host;x-amz-date;x-amz-target, Signature="
    )
    names = [
        "ApproximateReceiveCount",
        "ApproximateFirstReceiveTimestamp",
        "SentTimestamp",
        "MessageGroupId",
        "MessageDeduplicationId",
        "SequenceNumber",
    ]
    assert json.loads(request.content) == {
        "QueueUrl": QUEUE,
        "MaxNumberOfMessages": 5,
        "WaitTimeSeconds": 0,
        "VisibilityTimeout": 60,
        "AttributeNames": names,
        "MessageSystemAttributeNames": names,
        "MessageAttributeNames": ["All"],
    }


@pytest.mark.asyncio
async def test_fifo_receive_sends_attempt_id_and_empty_batch_is_empty() -> None:
    requests: list[httpx.Request] = []
    call = call_for(RECEIVE_MESSAGES_DEFINITION)
    result = await run_mutation(
        make_client(answer(httpx.Response(200, json={}), requests=requests)).receive_messages(),
        call,
        ReceiveMessagesInput(FIFO_QUEUE),
    )
    assert result.branch == EMPTY and result.value.is_fifo and result.value.messages == ()
    document = json.loads(requests[0].content)
    assert document["ReceiveRequestAttemptId"] == str(call.id) and document["MaxNumberOfMessages"] == 10
    assert document["WaitTimeSeconds"] == 20 and "VisibilityTimeout" not in document


@pytest.mark.parametrize(
    "body",
    [
        {"Messages": [sqs_message(MD5OfBody="0" * 32)]},
        {"Messages": [sqs_message(), sqs_message()]},
        {"Messages": [sqs_message(Body=None)]},
        {"Messages": [sqs_message(Attributes={"ApproximateReceiveCount": "0"})]},
        {"Messages": [sqs_message(Attributes={"ApproximateReceiveCount": "1\n"})]},
        {"Messages": [sqs_message(Attributes={"ApproximateReceiveCount": "1", "SentTimestamp": "1\n"})]},
        {"Messages": [sqs_message(MessageAttributes={"bad name": {"DataType": "String"}})]},
        {"Messages": [sqs_message(MessageAttributes={"kind": {"DataType": "Binary", "BinaryValue": "%%"}})]},
        {"Messages": "none"},
        {"Messages": [sqs_message(body=SECRET)]},
    ],
)
@pytest.mark.asyncio
async def test_invalid_receive_batches(body: object) -> None:
    result = await run_mutation(
        make_client(answer(httpx.Response(200, json=body))).receive_messages(),
        call_for(RECEIVE_MESSAGES_DEFINITION),
        ReceiveMessagesInput(QUEUE, max_messages=1),
    )
    assert result.branch == INVALID_RESPONSE and result.value.queue_url == QUEUE


@pytest.mark.parametrize(
    "value",
    [
        ReceiveMessagesInput(),
        ReceiveMessagesInput(QUEUE, max_messages=11),
        ReceiveMessagesInput(QUEUE, wait_time_seconds=21),
        ReceiveMessagesInput(QUEUE, visibility_timeout_seconds=-1),
        ReceiveMessagesInput("https://sqs.us-east-1.amazonaws.com/123456789012/orders"),
        ReceiveMessagesInput("https://sqs.eu-west-1.amazonaws.com/orders"),
        ReceiveMessagesInput(QUEUE + "?x=1"),
        ReceiveMessagesInput(QUEUE + "\n"),
        ReceiveMessagesInput(QUEUE.replace("orders", "ord\ners")),
        ReceiveMessagesInput(QUEUE.replace("orders", "ord\ters")),
        ReceiveMessagesInput(QUEUE.replace("orders", "ord\rers")),
        ReceiveMessagesInput(QUEUE.replace("orders", "ord ers")),
    ],
)
@pytest.mark.asyncio
async def test_invalid_receive_is_defect_without_request(value: ReceiveMessagesInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(answer(httpx.Response(200, json={}), requests=requests)).receive_messages(),
        call_for(RECEIVE_MESSAGES_DEFINITION),
        value,
    )
    assert result.branch == DEFECT and requests == []


def sqs_error(status: int, error_type: str = "", message: str = "", query_code: str = "") -> httpx.Response:
    headers = {"X-Amzn-Requestid": "req-error"}
    if query_code:
        headers["X-Amzn-Query-Error"] = query_code + ";Sender"
    return httpx.Response(status, json={"__type": error_type, "message": message}, headers=headers)


@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (
            sqs_error(400, query_code="AWS.SimpleQueueService.NonExistentQueue"),
            PROVIDER_REJECTED,
            FailureKind.NOT_FOUND,
        ),
        (sqs_error(400, "com.amazonaws.sqs#OverLimit"), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (
            sqs_error(400, "com.amazonaws.sqs#InvalidClientTokenId"),
            PROVIDER_REJECTED,
            FailureKind.AUTHENTICATION,
        ),
        (sqs_error(400, "RequestTimeTooSkewed"), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (sqs_error(403, "AccessDeniedException"), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (
            sqs_error(400, "com.amazonaws.sqs#ReceiptHandleIsInvalid"),
            RECEIPT_HANDLE_INVALID,
            FailureKind.NOT_FOUND,
        ),
        (
            sqs_error(400, "InvalidParameterValue", "Value for parameter VisibilityTimeout is invalid"),
            PROVIDER_REJECTED,
            FailureKind.PROVIDER_REJECTION,
        ),
        (
            sqs_error(400, "InvalidParameterValue", "The receipt handle has expired."),
            RECEIPT_HANDLE_INVALID,
            FailureKind.NOT_FOUND,
        ),
        (sqs_error(400, "Unknown:" + SECRET), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
    ],
)
@pytest.mark.asyncio
async def test_delete_error_classification(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_mutation(
        make_client(answer(response)).delete_message(),
        call_for(DELETE_MESSAGE_DEFINITION),
        DeleteMessageInput(QUEUE, "receipt-1", "order-1042"),
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind
    assert SECRET not in result.failure.message and "expired" not in result.failure.message
    assert result.receipt.provider_request_id == "req-error"
    assert result.receipt.provider_object_id == QUEUE + "#order-1042"


@pytest.mark.asyncio
async def test_error_metadata_rejects_trailing_newlines() -> None:
    response = httpx.Response(
        400,
        json={"__type": "InvalidClientTokenId\n"},
        headers={"X-Amzn-Requestid": "req-error\n", "X-Amzn-Query-Error": "InvalidClientTokenId\n;Sender"},
    )
    result = await run_mutation(
        make_client(answer(response)).delete_message(),
        call_for(DELETE_MESSAGE_DEFINITION),
        DeleteMessageInput(QUEUE, "receipt-1"),
    )
    assert result.branch == PROVIDER_REJECTED
    assert result.failure is not None and result.failure.kind == FailureKind.PROVIDER_REJECTION
    assert result.receipt.provider_request_id == ""


@pytest.mark.parametrize(
    ("response", "kind", "delay"),
    [
        (sqs_error(400, "com.amazon.coral.availability#ThrottlingException"), FailureKind.RATE_LIMIT, 0.0),
        (httpx.Response(429, headers={"Retry-After": "4"}), FailureKind.RATE_LIMIT, 4.0),
        (sqs_error(400, "RequestTimeout"), FailureKind.TRANSPORT, 0.0),
        (sqs_error(500, "InternalError"), FailureKind.AVAILABILITY, 0.0),
    ],
)
@pytest.mark.asyncio
async def test_retryable_errors(response: httpx.Response, kind: FailureKind, delay: float) -> None:
    with pytest.raises(RetryError) as raised:
        await run_mutation(
            make_client(answer(response)).delete_message(),
            call_for(DELETE_MESSAGE_DEFINITION),
            DeleteMessageInput(QUEUE, "receipt-1"),
        )
    assert raised.value.failure.kind == kind and raised.value.retry_after_seconds == delay


@pytest.mark.asyncio
async def test_delete_and_change_visibility_send_acknowledgements() -> None:
    requests: list[httpx.Request] = []
    deleted = await run_mutation(
        make_client(answer(httpx.Response(200, json={}), requests=requests)).delete_message(),
        call_for(DELETE_MESSAGE_DEFINITION),
        DeleteMessageInput(QUEUE, "receipt-1"),
    )
    assert deleted.branch == DELETED and deleted.receipt.provider_object_id == QUEUE
    assert requests[0].headers["X-Amz-Target"] == "AmazonSQS.DeleteMessage"
    assert json.loads(requests[0].content) == {"QueueUrl": QUEUE, "ReceiptHandle": "receipt-1"}
    changed = await run_mutation(
        make_client(answer(httpx.Response(200, json={}), requests=requests)).change_message_visibility(),
        call_for(CHANGE_MESSAGE_VISIBILITY_DEFINITION),
        ChangeMessageVisibilityInput(QUEUE, "receipt-1", "order-1042", 90),
    )
    assert changed.branch == CHANGED and changed.value.visible_after == NOW + timedelta(seconds=90)
    assert json.loads(requests[1].content) == {
        "QueueUrl": QUEUE,
        "ReceiptHandle": "receipt-1",
        "VisibilityTimeout": 90,
    }
    for invalid in (
        ChangeMessageVisibilityInput(QUEUE, "receipt-1", visibility_timeout_seconds=43201),
        ChangeMessageVisibilityInput(QUEUE, "receipt 1"),
        ChangeMessageVisibilityInput(QUEUE, "receipt-1", "order 1"),
    ):
        rejected = await run_mutation(
            make_client(answer(httpx.Response(200, json={}))).change_message_visibility(),
            call_for(CHANGE_MESSAGE_VISIBILITY_DEFINITION),
            invalid,
        )
        assert rejected.branch == DEFECT


@pytest.mark.asyncio
async def test_invalid_credentials_and_transport_failures() -> None:
    bad = StaticCredentialProvider(
        {CONNECTION: Credentials(SecretString("AKID/WRONG"), SecretString(SECRET), SecretString(""))}
    )
    result = await run_mutation(
        make_client(answer(httpx.Response(200, json={})), bad).delete_message(),
        call_for(DELETE_MESSAGE_DEFINITION),
        DeleteMessageInput(QUEUE, "receipt-1"),
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION

    def fail(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused", request=request)

    with pytest.raises(RetryError):
        await run_mutation(
            make_client(fail).receive_messages(),
            call_for(RECEIVE_MESSAGES_DEFINITION),
            ReceiveMessagesInput(QUEUE),
        )


def test_configuration_validation() -> None:
    provider = StaticCredentialProvider({CONNECTION: credentials()})
    assert make_client(answer(httpx.Response(200))).endpoint == "https://sqs.eu-west-1.amazonaws.com/"
    assert Client(Config(endpoint="", default_queue_url="", region="cn-north-1"), provider).endpoint == "https://sqs.cn-north-1.amazonaws.com.cn/"
    local_config = Config(endpoint="http://localhost:9324/queue", default_queue_url="", region="elasticmq")
    local = Client(local_config, provider)
    assert local.endpoint == "http://localhost:9324/queue" and not local.is_amazon_endpoint
    for config in (
        Config(endpoint="", default_queue_url="", region="moon-1"),
        Config(endpoint="http://sqs.example.com", default_queue_url="", region="eu-west-1"),
        Config(endpoint="", default_queue_url="", region="eu-west-1", max_messages_per_receive=11),
        Config(endpoint="", default_queue_url="https://example.com/orders", region="eu-west-1"),
        Config(endpoint="", default_queue_url="", region="eu-west-1\n"),
        Config(endpoint="", default_queue_url=QUEUE + "\n", region="eu-west-1"),
    ):
        with pytest.raises(ValueError):
            Client(config, provider)
    assert SECRET not in repr(credentials())


@pytest.mark.asyncio
async def test_project_connection_reads_stored_access_key(tmp_path: Path) -> None:
    path = tmp_path / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "amazon-sqs",
                        "connectionName": "amazon-sqs-orders",
                        "modulePath": (
                            "github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go"
                        ),
                        "provider": "amazon-sqs",
                        "configuration": {
                            "region": "eu-west-1",
                            "defaultQueueUrl": QUEUE,
                            "receiveWaitSeconds": 1,
                        },
                        "credentials": {"access_key_id": "AKIDEXAMPLE", "secret_access_key": SECRET},
                    }
                ],
                "triggerBindings": [],
            }
        )
    )
    path.chmod(0o600)
    project = await load_dex_web_project(path)
    requests: list[httpx.Request] = []
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(answer(httpx.Response(200, json={}), requests=requests))
    ) as http_client:
        connection = new_project_connection(project, "amazon-sqs-orders", http_client)
        call = Call(
            CallID(str(uuid4())),
            ConnectionRef("amazon-sqs", "amazon-sqs-orders"),
            RECEIVE_MESSAGES_DEFINITION.operation,
            IdempotencyKey("key"),
        )
        result = await run_mutation(connection.receive_messages(), call, ReceiveMessagesInput())
    assert result.branch == EMPTY
    assert json.loads(requests[0].content)["WaitTimeSeconds"] == 1
    assert "Credential=AKIDEXAMPLE/" in requests[0].headers["Authorization"]
    await project.close()
