# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail read operation classifications against bounded HTTP responses."""

import base64
import json
from datetime import UTC, datetime
from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    CredentialRefreshResult,
    CredentialRefreshState,
    FailureKind,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.google_gmail import (
    Config,
    Credentials,
    GetMessageInput,
    GetMessageOperation,
    GmailProvider,
)


class NoRefreshDriver:
    """Prevent provider token exchange in deterministic read tests."""

    def refresh_required(self, state: CredentialRefreshState[Credentials]) -> bool:
        """The fixture credential remains fresh."""
        return False

    async def refresh(
        self, state: CredentialRefreshState[Credentials]
    ) -> CredentialRefreshResult[Credentials]:
        """Fail if a read unexpectedly exchanges credentials."""
        raise AssertionError("unexpected Gmail credential refresh")


@pytest.fixture
def call() -> Call:
    """Create a distinct read call and stable logical connection."""
    return Call(CallID(str(uuid4())), ConnectionRef("google", "support"), OperationRef("gmail", "getMessage"))


def message_response() -> bytes:
    """Return one full Gmail message with decoded text and HTML children."""
    text = base64.urlsafe_b64encode(b"Hello").rstrip(b"=").decode()
    html = base64.urlsafe_b64encode(b"<p>Hello</p>").rstrip(b"=").decode()
    return json.dumps(
        {
            "id": "message-1",
            "threadId": "thread-1",
            "internalDate": "1772615700000",
            "snippet": "Hello",
            "labelIds": ["INBOX"],
            "payload": {
                "headers": [
                    {"name": "From", "value": "Ada <ada@example.com>"},
                    {"name": "Reply-To", "value": "reply@example.com"},
                    {"name": "Message-ID", "value": "<source@example.com>"},
                    {"name": "To", "value": "support@example.org"},
                    {"name": "Subject", "value": "=?UTF-8?Q?Hello?="},
                ],
                "mimeType": "multipart/alternative",
                "parts": [
                    {"mimeType": "text/plain", "body": {"data": text}},
                    {"mimeType": "text/html", "body": {"data": html}},
                ],
            },
        }
    ).encode()


def make_operation(
    call: Call, response: httpx.Response, *, max_response_bytes: int = 1024
) -> GetMessageOperation:
    """Serve one provider response without network access or credential mutation."""
    credentials = StaticCredentialProvider(
        {call.connection: Credentials(SecretString("access-token"), "owner@example.org")}
    )

    def serve(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/gmail/v1/users/me/messages/message-1"
        assert request.url.params["format"] == "full"
        assert request.headers["Authorization"] == "Bearer access-token"
        return response

    http_client = httpx.AsyncClient(transport=httpx.MockTransport(serve))
    provider = GmailProvider(
        Config(endpoint="http://localhost/gmail/v1", max_response_bytes=max_response_bytes),
        credentials,
        NoRefreshDriver(),
        http_client,
    )
    return GetMessageOperation(provider)


@pytest.mark.asyncio
async def test_get_message_decodes_bodies_headers_and_receipt(call: Call) -> None:
    """A valid 200 selects read and keeps provider identity in its receipt."""
    operation = make_operation(
        call, httpx.Response(200, content=message_response(), headers={"X-Goog-Request-Id": "req-1"})
    )
    result = await operation.invoke(call, GetMessageInput("message-1"))
    assert result.kind == "branch" and result.branch == "read" and result.failure is None
    assert result.value is not None
    assert result.value.message_id == "message-1"
    assert result.value.text_body == "Hello"
    assert result.value.html_body == "<p>Hello</p>"
    assert result.value.from_address == "Ada <ada@example.com>"
    assert result.value.received_at == datetime(2026, 3, 4, 9, 15, tzinfo=UTC)
    assert result.receipt is not None and result.receipt.provider_request_id == "req-1"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "branch", "kind"),
    [
        (404, "notFound", None),
        (401, "providerRejected", FailureKind.AUTHENTICATION),
        (403, "providerRejected", FailureKind.AUTHORIZATION),
        (409, "providerRejected", FailureKind.CONFLICT),
        (400, "providerRejected", FailureKind.PROVIDER_REJECTION),
    ],
)
async def test_get_message_terminal_refusals(
    call: Call,
    status: int,
    branch: str,
    kind: FailureKind | None,
) -> None:
    """Definite Gmail refusals select the same branches and kinds as Go."""
    result = await make_operation(call, httpx.Response(status)).invoke(call, GetMessageInput("message-1"))
    assert result.kind == "branch" and result.branch == branch
    assert (result.failure.kind if result.failure else None) == kind
    assert result.receipt is not None and result.receipt.provider == ""


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [429, 500, 503])
async def test_get_message_retry_after(call: Call, status: int) -> None:
    """Throttling and availability failures retry after Gmail's integer delay."""
    result = await make_operation(call, httpx.Response(status, headers={"Retry-After": "3"})).invoke(
        call, GetMessageInput("message-1")
    )
    assert result.kind == "retry" and result.failure is not None
    assert result.failure.kind == FailureKind.AVAILABILITY
    assert result.retry_after_seconds == 3


@pytest.mark.asyncio
async def test_get_message_invalid_retry_after_is_protocol_retry(call: Call) -> None:
    """A non-integer Retry-After value never becomes a terminal branch."""
    result = await make_operation(call, httpx.Response(429, headers={"Retry-After": "later"})).invoke(
        call, GetMessageInput("message-1")
    )
    assert result.kind == "retry" and result.failure is not None
    assert result.failure.kind == FailureKind.PROTOCOL


@pytest.mark.asyncio
async def test_get_message_signed_nonnegative_retry_after_matches_go(call: Call) -> None:
    """Go accepts an explicit plus sign in a whole-second Retry-After value."""
    result = await make_operation(call, httpx.Response(429, headers={"Retry-After": "+3"})).invoke(
        call, GetMessageInput("message-1")
    )
    assert result.kind == "retry" and result.failure is not None
    assert result.failure.kind == FailureKind.AVAILABILITY
    assert result.retry_after_seconds == 3


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        b"not json",
        b'{"id":"message-1"}',
        b'{"id":"message-1","threadId":"t","internalDate":"bad"}',
        b'{"id":"message-1","threadId":"t","internalDate":1772615700000}',
        b'{"id":"message-1","threadId":"t","internalDate":"1772615700000","payload":{"mimeType":"text/plain","body":{"data":"@invalid"}}}',
    ],
)
async def test_get_message_invalid_payload(call: Call, body: bytes) -> None:
    """Malformed 2xx content selects invalidResponse with a protocol failure."""
    result = await make_operation(call, httpx.Response(200, content=body)).invoke(
        call, GetMessageInput("message-1")
    )
    assert result.kind == "branch" and result.branch == "invalidResponse"
    assert result.failure is not None and result.failure.kind == FailureKind.PROTOCOL
    assert result.receipt is not None
    if body == b"not json":
        assert result.failure.message == "Gmail returned an invalid or oversized message response"
        assert result.receipt.provider == ""
    else:
        assert result.failure.message == "Gmail returned an invalid message"
        assert result.receipt.provider == "gmail"


@pytest.mark.asyncio
async def test_get_message_oversized_response(call: Call) -> None:
    """A complete response above the configured bound is invalidResponse."""
    result = await make_operation(
        call, httpx.Response(200, content=message_response()), max_response_bytes=10
    ).invoke(call, GetMessageInput("message-1"))
    assert result.kind == "branch" and result.branch == "invalidResponse"
    assert result.failure is not None and result.failure.kind == FailureKind.PROTOCOL


@pytest.mark.asyncio
async def test_get_message_broken_success_stream_is_invalid_response(call: Call) -> None:
    """A 2xx stream read failure remains a protocol branch with its known status."""

    class BrokenStream(httpx.AsyncByteStream):
        async def __aiter__(self):
            raise httpx.ReadError("inert broken body")
            yield b""

    result = await make_operation(call, httpx.Response(200, stream=BrokenStream())).invoke(
        call, GetMessageInput("message-1")
    )
    assert result.branch == "invalidResponse" and result.failure is not None
    assert result.failure.kind == FailureKind.PROTOCOL


@pytest.mark.asyncio
async def test_get_message_empty_id_never_reaches_provider(call: Call) -> None:
    """Local validation selects defect without making an HTTP request."""
    operation = make_operation(call, httpx.Response(500))
    result = await operation.invoke(call, GetMessageInput(""))
    assert result.branch == "defect"
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION


@pytest.mark.asyncio
async def test_get_message_missing_connection_is_authentication_defect(call: Call) -> None:
    """Missing credential material remains a local authentication defect."""
    provider = GmailProvider(Config(), StaticCredentialProvider({}), NoRefreshDriver())
    result = await GetMessageOperation(provider).invoke(call, GetMessageInput("message-1"))
    assert result.branch == "defect"
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    await provider.close()
