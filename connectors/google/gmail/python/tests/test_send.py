# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail send and reply read-back classification against a fake HTTP provider."""

import base64
import json
from datetime import UTC, datetime, timedelta
from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, FailureKind, IdempotencyKey, OperationRef
from dex_connectors.sdk.credentials import SecretString, StaticCredentialProvider

from dex_connectors.google_gmail import (
    Config,
    Credentials,
    GmailProvider,
    ReplyToMessageInput,
    SendMessageInput,
)
from dex_connectors.google_gmail.provider import build_send_message
from dex_connectors.google_gmail.send import SendExecution, SendService


def execution(operation_id: str = "sendMessage", attempt: int = 1) -> SendExecution:
    """Supply explicit Dex attempt facts until the released Context exposes them."""
    call_id = CallID(str(uuid4()))
    connection = ConnectionRef("google", "support")
    call = Call(call_id, connection, OperationRef("gmail", operation_id), IdempotencyKey(call_id))
    return SendExecution(
        call,
        attempt,
        "run-1",
        datetime(2026, 3, 4, 9, 15, tzinfo=UTC),
        datetime.now(UTC) + timedelta(seconds=30),
        timedelta(seconds=30),
    )


def send_input() -> SendMessageInput:
    """Use one valid recipient and simple body."""
    return SendMessageInput(["ada@example.com"], "Progress", "Update")


@pytest.mark.parametrize(
    ("subject", "expected_header"),
    [
        ("Progress", b"Subject: Progress\r\n"),
        ("Caf\u00e9 and tea", b"Subject: =?UTF-8?q?Caf=C3=A9_and_tea?=\r\n"),
    ],
)
def test_send_subject_uses_go_mime_q_encoding(subject: str, expected_header: bytes) -> None:
    """The Gmail MIME request keeps Go's plain ASCII and UTF-8 Q-encoded subjects."""
    run = execution()
    payload = build_send_message(
        run.call,
        "owner@example.org",
        ["ada@example.com"],
        SendMessageInput(["ada@example.com"], subject, "Update"),
        run.run_id,
        run.flow_started_at,
    )
    encoded = json.loads(payload)["raw"]
    raw = base64.urlsafe_b64decode(encoded + "=" * (-len(encoded) % 4))
    assert expected_header in raw


def source_response() -> httpx.Response:
    """Return source metadata required for an RFC-threaded reply."""
    return httpx.Response(
        200,
        json={
            "id": "source-1",
            "threadId": "thread-1",
            "internalDate": "1772615700000",
            "payload": {
                "headers": [
                    {"name": "From", "value": "Ada <ada@example.com>"},
                    {"name": "Reply-To", "value": "ada.reply@example.com"},
                    {"name": "Message-ID", "value": "<source@example.com>"},
                    {"name": "Subject", "value": "Question"},
                ]
            },
        },
    )


def service(
    execution_value: SendExecution,
    serve: httpx.MockTransport,
    max_response_bytes: int = 1024,
    max_message_bytes: int = 10_485_760,
) -> SendService:
    """Bind a static logical connection and bounded mock HTTP transport."""
    credential = Credentials(SecretString("access-token"), "owner@example.org")
    credentials = StaticCredentialProvider({execution_value.call.connection: credential})
    http_client = httpx.AsyncClient(transport=serve)
    provider = GmailProvider(
        Config(
            endpoint="http://localhost/gmail/v1",
            max_response_bytes=max_response_bytes,
            max_message_bytes=max_message_bytes,
        ),
        credentials,
        http_client=http_client,
    )
    return SendService(provider)


@pytest.mark.asyncio
async def test_send_success_posts_one_json_message() -> None:
    """A complete 2xx returns sent without a Sent Mail query."""
    run = execution()
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "POST"
        assert request.url.path == "/gmail/v1/users/me/messages/send"
        assert request.headers["Authorization"] == "Bearer access-token"
        payload = json.loads(request.content)
        assert set(payload) == {"raw"}
        raw = base64.urlsafe_b64decode(payload["raw"] + "=" * (-len(payload["raw"]) % 4))
        assert b"X-Dex-Call-ID: " + str(run.call.id).encode() in raw
        return httpx.Response(
            200, json={"id": "sent-1", "threadId": "thread-1"}, headers={"X-Goog-Request-Id": "req-1"}
        )

    result = await service(run, httpx.MockTransport(answer)).send_message(run, send_input())
    assert result.kind == "branch" and result.branch == "sent" and result.failure is None
    assert result.value is not None and result.value.message_id == "sent-1"
    assert result.receipt is not None and result.receipt.provider_request_id == "req-1"
    assert len(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind", "delay"),
    [
        (429, FailureKind.RATE_LIMIT, 3),
        (503, FailureKind.AVAILABILITY, 3),
    ],
)
async def test_send_retryable_status(status: int, kind: FailureKind, delay: int) -> None:
    """429 and 503 remain Retry with the provider's integer Retry-After."""
    run = execution()

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return httpx.Response(200, json={"messages": []})
        return httpx.Response(status, headers={"Retry-After": str(delay)})

    result = await service(run, httpx.MockTransport(answer)).send_message(run, send_input())
    assert result.kind == "retry" and result.failure is not None
    assert result.failure.kind == kind and result.retry_after_seconds == delay


@pytest.mark.asyncio
async def test_send_invalid_retry_after_is_protocol_retry() -> None:
    """Malformed Retry-After on 429 remains retryable with a protocol failure."""
    run = execution()
    result = await service(
        run, httpx.MockTransport(lambda _: httpx.Response(429, headers={"Retry-After": "tomorrow"}))
    ).send_message(run, send_input())
    assert result.kind == "retry" and result.failure is not None
    assert result.failure.kind == FailureKind.PROTOCOL


@pytest.mark.asyncio
async def test_send_signed_nonnegative_retry_after_matches_go() -> None:
    """Go accepts a plus sign before an integer Retry-After value."""
    run = execution()
    result = await service(
        run, httpx.MockTransport(lambda _: httpx.Response(429, headers={"Retry-After": "+3"}))
    ).send_message(run, send_input())
    assert result.kind == "retry" and result.failure is not None
    assert result.failure.kind == FailureKind.RATE_LIMIT
    assert result.retry_after_seconds == 3


@pytest.mark.asyncio
async def test_send_503_readback_match_is_sent() -> None:
    """A Sent Mail match after 503 proves acceptance before any resend."""
    run = execution()
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        if request.method == "GET":
            assert request.url.params["includeSpamTrash"] == "true"
            assert request.url.params["q"].startswith("in:sent rfc822msgid:")
            return httpx.Response(200, json={"messages": [{"id": "sent-1", "threadId": "thread-1"}]})
        return httpx.Response(503)

    result = await service(run, httpx.MockTransport(answer)).send_message(run, send_input())
    assert result.branch == "sent" and result.value is not None and result.value.message_id == "sent-1"
    assert methods == ["POST", "GET"]


@pytest.mark.asyncio
async def test_send_ambiguous_500_search_miss_retries() -> None:
    """An unconfirmed possibly accepted send retries after four bounded lookups."""
    run = execution()
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        return httpx.Response(500) if request.method == "POST" else httpx.Response(200, json={"messages": []})

    result = await service(run, httpx.MockTransport(answer)).send_message(run, send_input())
    assert result.kind == "retry" and result.failure is not None
    assert result.failure.kind == FailureKind.AVAILABILITY
    assert methods == ["POST", "GET", "GET", "GET", "GET"]


@pytest.mark.asyncio
async def test_send_ambiguous_500_search_failure_retries_all_lookups() -> None:
    """A failed lookup remains retryable after the full bounded read-back window."""
    run = execution()
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        return httpx.Response(500)

    result = await service(run, httpx.MockTransport(answer)).send_message(run, send_input())
    assert result.kind == "retry" and result.failure is not None
    assert result.failure.kind == FailureKind.AVAILABILITY
    assert result.failure.message.endswith("the sent-mail search did not answer")
    assert methods == ["POST", "GET", "GET", "GET", "GET"]


@pytest.mark.asyncio
async def test_second_send_attempt_searches_before_post() -> None:
    """A prior accepted message prevents duplicate POST on numbered retries."""
    run = execution(attempt=2)
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        assert request.method == "GET"
        return httpx.Response(200, json={"messages": [{"id": "prior-1", "threadId": "thread-1"}]})

    result = await service(run, httpx.MockTransport(answer)).send_message(run, send_input())
    assert result.branch == "sent" and result.value is not None and result.value.message_id == "prior-1"
    assert methods == ["GET"]


@pytest.mark.asyncio
async def test_missing_id_in_sent_search_does_not_escape_4xx_classification() -> None:
    """A no-ID Sent match is ignored before a definite provider rejection."""
    run = execution(attempt=2)
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        if request.method == "GET":
            return httpx.Response(200, json={"messages": [{"threadId": "thread-1"}]})
        return httpx.Response(400)

    result = await service(run, httpx.MockTransport(answer)).send_message(run, send_input())
    assert result.branch == "providerRejected"
    assert methods == ["GET", "POST", "GET"]


@pytest.mark.asyncio
async def test_accepted_unusable_response_is_sent_without_resend() -> None:
    """A 2xx without an ID is accepted even when Sent Mail has not indexed it."""
    run = execution()
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        return httpx.Response(
            200, json={"threadId": "ignored"} if request.method == "POST" else {"messages": []}
        )

    result = await service(run, httpx.MockTransport(answer)).send_message(run, send_input())
    assert result.branch == "sent" and result.value is not None and result.value.message_id == ""
    assert methods == ["POST", "GET", "GET", "GET", "GET"]


@pytest.mark.asyncio
async def test_send_auth_refusal_and_local_validation() -> None:
    """A 401 is a terminal provider rejection; an empty recipient is a local defect."""
    run = execution()
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        return httpx.Response(401)

    sender = service(run, httpx.MockTransport(answer))
    refused = await sender.send_message(run, send_input())
    invalid = await sender.send_message(run, SendMessageInput([], "Progress", "Update"))
    assert refused.branch == "providerRejected" and refused.failure is not None
    assert refused.failure.kind == FailureKind.AUTHENTICATION
    assert invalid.branch == "defect" and invalid.failure is not None
    assert invalid.failure.kind == FailureKind.VALIDATION
    assert methods == ["POST"]


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("input_value", "failure_message"),
    [
        (
            SendMessageInput([], "Progress", "Update"),
            "sender, recipients, subject, and text body are required",
        ),
        (SendMessageInput(["invalid"], "Progress", "Update"), "recipient email is invalid"),
        (
            SendMessageInput(["Ada <ada@example.com>", "ada@example.com"], "Progress", "Update"),
            "recipient email is duplicated",
        ),
        (
            SendMessageInput(["ada@example.com"], "Hello\r\nBcc: other@example.com", "Update"),
            "sender, recipients, subject, and text body are required",
        ),
    ],
)
async def test_send_local_validation_matches_go_failure(
    input_value: SendMessageInput, failure_message: str
) -> None:
    """Invalid local fields select the exact Go failure before provider I/O."""
    run = execution()

    def unexpected_request(_: httpx.Request) -> httpx.Response:
        raise AssertionError("local validation must not send a request")

    result = await service(run, httpx.MockTransport(unexpected_request)).send_message(run, input_value)
    assert result.branch == "defect" and result.failure is not None
    assert result.failure.message == failure_message
    assert result.receipt is not None and result.receipt.provider == ""


@pytest.mark.asyncio
async def test_send_oversized_request_is_local_validation_failure() -> None:
    """The MIME size cap prevents dispatch and returns the Go size failure."""
    run = execution()

    def unexpected_request(_: httpx.Request) -> httpx.Response:
        raise AssertionError("oversized request must not be sent")

    sender = service(run, httpx.MockTransport(unexpected_request), max_message_bytes=20)
    result = await sender.send_message(run, send_input())
    assert result.branch == "defect" and result.failure is not None
    assert result.failure.message == "message exceeds configured size limit"


@pytest.mark.asyncio
async def test_send_refreshes_once_after_401() -> None:
    """A rejected access token is refreshed once before the same logical POST retries."""
    run = execution()
    tokens: list[str] = []

    class RotatingCredentialProvider:
        async def resolve(self, call: Call) -> Credentials:
            return Credentials(SecretString("old-access"), "owner@example.org")

        async def resolve_with_refresh(self, call: Call, driver: object) -> Credentials:
            return await self.resolve(call)

        async def resolve_after_rejection(self, call: Call, driver: object) -> Credentials:
            return Credentials(SecretString("new-access"), "owner@example.org")

    def answer(request: httpx.Request) -> httpx.Response:
        tokens.append(request.headers["Authorization"])
        return (
            httpx.Response(401)
            if len(tokens) == 1
            else httpx.Response(200, json={"id": "sent-1", "threadId": "thread-1"})
        )

    client = httpx.AsyncClient(transport=httpx.MockTransport(answer))
    provider = GmailProvider(
        Config(endpoint="http://localhost/gmail/v1"), RotatingCredentialProvider(), http_client=client
    )
    result = await SendService(provider).send_message(run, send_input())
    assert result.branch == "sent"
    assert tokens == ["Bearer old-access", "Bearer new-access"]


@pytest.mark.asyncio
async def test_accepted_oversized_response_remains_sent() -> None:
    """An oversized 2xx has already been accepted and must not be posted again."""
    run = execution()
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        return httpx.Response(200, content=b"{" + b"x" * 64 + b"}")

    result = await service(run, httpx.MockTransport(answer), max_response_bytes=10).send_message(
        run, send_input()
    )
    assert result.branch == "sent" and result.value is not None and result.value.message_id == ""
    assert methods.count("POST") == 1


@pytest.mark.asyncio
async def test_accepted_broken_stream_remains_sent() -> None:
    """A 2xx body read failure cannot cause another POST."""
    run = execution()
    methods: list[str] = []

    class BrokenStream(httpx.AsyncByteStream):
        async def __aiter__(self):
            raise httpx.ReadError("inert broken body")
            yield b""

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        return (
            httpx.Response(200, stream=BrokenStream())
            if request.method == "POST"
            else httpx.Response(200, json={"messages": []})
        )

    result = await service(run, httpx.MockTransport(answer)).send_message(run, send_input())
    assert result.branch == "sent" and result.value is not None and result.value.message_id == ""
    assert methods.count("POST") == 1


@pytest.mark.asyncio
async def test_transport_failure_without_not_sent_proof_uses_readback() -> None:
    """An untraced transport failure may have reached Gmail and requires read-back."""
    run = execution()
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        if request.method == "POST":
            raise httpx.ConnectError("inert connection loss")
        return httpx.Response(200, json={"messages": []})

    result = await service(run, httpx.MockTransport(answer)).send_message(run, send_input())
    assert result.kind == "retry" and result.failure is not None
    assert result.failure.kind == FailureKind.TRANSPORT
    assert methods == ["POST", "GET", "GET", "GET", "GET"]


@pytest.mark.asyncio
async def test_reply_reads_source_then_sends_threaded_payload() -> None:
    """A reply reads metadata once and posts the source thread ID and headers."""
    run = execution("replyToMessage")
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        if request.method == "GET":
            assert request.url.params["format"] == "metadata"
            return source_response()
        payload = json.loads(request.content)
        assert payload["threadId"] == "thread-1"
        raw = base64.urlsafe_b64decode(payload["raw"] + "=" * (-len(payload["raw"]) % 4))
        assert b"In-Reply-To: <source@example.com>" in raw
        return httpx.Response(200, json={"id": "reply-1", "threadId": "thread-1"})

    result = await service(run, httpx.MockTransport(answer)).reply_to_message(
        run, ReplyToMessageInput("source-1", "Answer")
    )
    assert result.branch == "sent" and result.value is not None
    assert result.value.recipients == ["ada.reply@example.com"]
    assert methods == ["GET", "POST"]


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), "providerRejected", FailureKind.NOT_FOUND),
        (httpx.Response(200, json={"id": "source-1"}), "invalidResponse", FailureKind.PROTOCOL),
        (httpx.Response(429, headers={"Retry-After": "2"}), "", FailureKind.AVAILABILITY),
    ],
)
async def test_reply_source_classification(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    """Source lookup branches before any reply POST."""
    run = execution("replyToMessage")
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        return response

    result = await service(run, httpx.MockTransport(answer)).reply_to_message(
        run, ReplyToMessageInput("source-1", "Answer")
    )
    assert result.branch == branch and result.failure is not None and result.failure.kind == kind
    assert methods == ["GET"]


@pytest.mark.asyncio
async def test_reply_invalid_json_matches_go_source_protocol_failure() -> None:
    """A malformed source JSON selects invalidResponse before any reply POST."""
    run = execution("replyToMessage")
    result = await service(
        run, httpx.MockTransport(lambda _: httpx.Response(200, content=b"not json"))
    ).reply_to_message(run, ReplyToMessageInput("source-1", "Answer"))
    assert result.branch == "invalidResponse" and result.failure is not None
    assert result.failure.message == "Gmail returned an invalid source message response"
    assert result.receipt is not None and result.receipt.provider_object_id == "source-1"


@pytest.mark.asyncio
async def test_reply_source_invalid_retry_after_is_protocol_retry() -> None:
    """An invalid provider delay cannot turn source throttling into a branch."""
    run = execution("replyToMessage")
    result = await service(
        run, httpx.MockTransport(lambda _: httpx.Response(429, headers={"Retry-After": "later"}))
    ).reply_to_message(run, ReplyToMessageInput("source-1", "Answer"))
    assert result.kind == "retry" and result.failure is not None
    assert result.failure.kind == FailureKind.PROTOCOL


@pytest.mark.asyncio
async def test_reply_send_authentication_refusal_is_terminal() -> None:
    """A definite 401 after source lookup selects providerRejected for the reply."""
    run = execution("replyToMessage")
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        return source_response() if request.method == "GET" else httpx.Response(401)

    result = await service(run, httpx.MockTransport(answer)).reply_to_message(
        run, ReplyToMessageInput("source-1", "Answer")
    )
    assert result.branch == "providerRejected" and result.failure is not None
    assert result.failure.kind == FailureKind.AUTHENTICATION
    assert methods == ["GET", "POST"]


@pytest.mark.asyncio
async def test_reply_accepted_oversized_response_remains_sent() -> None:
    """An oversized accepted reply response never causes another POST."""
    run = execution("replyToMessage")
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        if request.url.params.get("format") == "metadata":
            return source_response()
        if request.method == "POST":
            return httpx.Response(200, content=b"{" + b"x" * 1024 + b"}")
        return httpx.Response(200, json={"messages": []})

    result = await service(run, httpx.MockTransport(answer), max_response_bytes=512).reply_to_message(
        run, ReplyToMessageInput("source-1", "Answer")
    )
    assert result.branch == "sent" and result.value is not None
    assert result.value.message_id == "" and result.value.thread_id == "thread-1"
    assert methods.count("POST") == 1


@pytest.mark.asyncio
async def test_reply_empty_body_is_local_defect_without_provider_io() -> None:
    """A required blank reply body selects defect before any source lookup."""
    run = execution("replyToMessage")

    def unexpected_request(_: httpx.Request) -> httpx.Response:
        raise AssertionError("blank reply must not reach Gmail")

    result = await service(run, httpx.MockTransport(unexpected_request)).reply_to_message(
        run, ReplyToMessageInput("source-1", "")
    )
    assert result.branch == "defect" and result.failure is not None
    assert result.failure.kind == FailureKind.VALIDATION
    assert result.receipt is not None and result.receipt.provider == ""


@pytest.mark.asyncio
async def test_reply_missing_credentials_is_authentication_defect() -> None:
    """A missing logical connection never reaches Gmail."""
    run = execution("replyToMessage")
    provider = GmailProvider(Config(), StaticCredentialProvider({}))
    try:
        result = await SendService(provider).reply_to_message(run, ReplyToMessageInput("source-1", "Answer"))
    finally:
        await provider.close()
    assert result.branch == "defect" and result.failure is not None
    assert result.failure.kind == FailureKind.AUTHENTICATION


@pytest.mark.asyncio
async def test_reply_oversized_source_is_invalid_response() -> None:
    """An oversized source response cannot produce a reply POST."""
    run = execution("replyToMessage")
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        return source_response()

    result = await service(run, httpx.MockTransport(answer), max_response_bytes=10).reply_to_message(
        run, ReplyToMessageInput("source-1", "Answer")
    )
    assert result.branch == "invalidResponse" and result.failure is not None
    assert result.failure.kind == FailureKind.PROTOCOL
    assert methods == ["GET"]


@pytest.mark.asyncio
async def test_second_reply_attempt_readback_match_skips_source_and_post() -> None:
    """A prior Sent Mail match wins before reading the reply source on retry."""
    run = execution("replyToMessage", 2)
    methods: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        assert request.url.path == "/gmail/v1/users/me/messages"
        return httpx.Response(200, json={"messages": [{"id": "reply-1", "threadId": "thread-1"}]})

    result = await service(run, httpx.MockTransport(answer)).reply_to_message(
        run, ReplyToMessageInput("source-1", "Answer")
    )
    assert result.branch == "sent" and result.value is not None and result.value.recipients == []
    assert methods == ["GET"]
