# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fenced replyToConversation decisions after accepted, unknown, and refused sends."""

import asyncio
import json
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
from dex_connectors.sdk.singledispatch import Marker, State

from dex_connectors.helpscout import Client, Config, Credentials
from dex_connectors.helpscout.models import ReplyToConversationInput

CONNECTION = sdk.ConnectionRef("helpscout", "helpscout-test")
CALL_ID = sdk.CallID("3f9f4c0b-1d7f-5034-a855-40824c3de676")
NOTE = ReplyToConversationInput(501, "Triage note", True)
NOTES_PATH = "/v2/conversations/501/notes"

Responder = Callable[[httpx.Request], httpx.Response]


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, deadline_seconds: float = 30) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=deadline_seconds)
        self.first_attempt_at = datetime(2026, 1, 1, tzinfo=UTC)
        self.heartbeat_value: object = None

    @property
    def marker(self) -> Marker | None:
        """Decode this call's checkpoint from either SDK heartbeat format."""
        if isinstance(self.heartbeat_value, Marker) or self.heartbeat_value is None:
            return self.heartbeat_value
        assert isinstance(self.heartbeat_value, dict)
        entry = self.heartbeat_value["singleDispatchMarkers"].get(str(CALL_ID))
        return None if entry is None else Marker.from_wire(entry)

    def advance(self) -> None:
        """Start the next Execute attempt with a fresh deadline."""
        self.attempt += 1
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat as the reading SDK expects it."""
        seeded = self.heartbeat_value
        if isinstance(seeded, Marker) and value_type is not Marker:
            return {"singleDispatchMarkers": {str(seeded.call_id): seeded.to_wire()}}
        if isinstance(seeded, dict) and value_type is Marker:
            return self.marker
        return seeded

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value


class BrokenBody(httpx.AsyncByteStream):
    """A body that fails after the status and headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b"partial"
        raise httpx.ReadError("connection reset")


class HangingBody(httpx.AsyncByteStream):
    """A body that never finishes, so the write window cuts it off."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        await asyncio.sleep(30)
        yield b""


def error_response(status: int) -> httpx.Response:
    """Answer with Help Scout's error object."""
    return httpx.Response(status, content=json.dumps({"message": "error"}).encode())


async def run(
    responder: Responder,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    input: ReplyToConversationInput = NOTE,
    credentials: sdk.CredentialProvider[Credentials] | None = None,
) -> sdk.MutationResult[object] | str:
    """Run one Execute attempt and return its result, or `retry`."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return responder(request)

    source = credentials or sdk.StaticCredentialProvider(
        {
            CONNECTION: Credentials(
                "app-id",
                sdk.SecretString("app-secret"),
                sdk.SecretString("inert-access-token"),
                sdk.SecretString("webhook-secret"),
            )
        }
    )
    client = Client(Config(), source, httpx.AsyncClient(transport=httpx.MockTransport(respond)))
    call = sdk.Call(
        CALL_ID,
        CONNECTION,
        sdk.OperationRef("helpscout", "replyToConversation"),
        context=context,
        max_sends=3,
    )
    try:
        return await sdk.run_mutation(client.reply_to_conversation(), call, input)
    except sdk.RetryError:
        return "retry"


def branch_of(result: sdk.MutationResult[object] | str) -> str:
    """Return the selected branch or `retry`."""
    return result if isinstance(result, str) else result.branch


def posts(requests: list[httpx.Request]) -> int:
    """Count thread POSTs."""
    return sum(1 for request in requests if request.method == "POST")


async def test_accepted_reply_without_a_thread_id_is_uncertain_and_never_resent() -> None:
    requests: list[httpx.Request] = []
    result = await run(
        lambda _: httpx.Response(201, headers={"Resource-Id": "not-a-number"}), AttemptContext(), requests
    )
    assert branch_of(result) == "uncertain"
    assert posts(requests) == 1


async def test_accepted_reply_whose_body_breaks_keeps_the_thread_id() -> None:
    requests: list[httpx.Request] = []
    result = await run(
        lambda _: httpx.Response(201, headers={"Resource-Id": "568"}, stream=BrokenBody()),
        AttemptContext(),
        requests,
    )
    assert not isinstance(result, str)
    assert result.branch == "replied"
    assert result.value.thread_id == 568  # type: ignore[attr-defined]
    assert posts(requests) == 1


async def test_accepted_reply_cut_off_by_the_write_window_keeps_the_thread_id() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(deadline_seconds=0.6)
    result = await run(
        lambda _: httpx.Response(201, headers={"Resource-Id": "568"}, stream=HangingBody()), context, requests
    )
    assert not isinstance(result, str)
    assert result.branch == "replied"
    assert result.value.thread_id == 568  # type: ignore[attr-defined]
    assert posts(requests) == 1


async def test_refusal_after_a_rate_limited_resend_is_uncertain() -> None:
    statuses = iter([502, 429, 422])
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run(lambda _: error_response(next(statuses)), context, requests) == "retry"
    context.advance()
    assert await run(lambda _: error_response(next(statuses)), context, requests) == "retry"
    assert context.marker is not None and context.marker.state == State.CAN_RESEND
    context.advance()
    result = await run(lambda _: error_response(next(statuses)), context, requests)
    assert branch_of(result) == "uncertain"
    assert posts(requests) == 3


async def test_locked_conversation_status_is_an_unknown_outcome() -> None:
    for status in (412, 423):
        requests: list[httpx.Request] = []
        context = AttemptContext()
        assert await run(lambda _, code=status: error_response(code), context, requests) == "retry"
        assert context.marker is not None and context.marker.state == State.CLAIMED
        assert posts(requests) == 1


class FailingCredentials:
    """Fail every credential resolution with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> Credentials:
        """Raise the configured error."""
        raise self.error


async def test_credential_failure_after_a_possible_send_records_nothing() -> None:
    for error, expected in (
        (RuntimeError("token endpoint unavailable"), "retry"),
        (sdk.ReauthorizationRequiredError("revoked"), "uncertain"),
    ):
        requests: list[httpx.Request] = []
        context = AttemptContext()
        assert await run(lambda _: error_response(502), context, requests) == "retry"
        context.advance()
        failing = FailingCredentials(error)
        result = await run(lambda _: error_response(502), context, requests, credentials=failing)  # type: ignore[arg-type]
        assert branch_of(result) == expected
        assert context.marker is not None and context.marker.state == State.CLAIMED
        assert posts(requests) == 1


async def test_primary_customer_read_retry_after_a_possible_send_records_nothing() -> None:
    conversation = {
        "id": 501,
        "number": 12,
        "threads": 1,
        "type": "email",
        "status": "active",
        "state": "published",
        "subject": "Billing question",
        "mailboxId": 123,
        "primaryCustomer": {"id": 238604, "type": "customer", "email": "jane@acme.example.com"},
        "createdAt": "2026-09-29T22:46:22Z",
    }
    reads = iter([httpx.Response(200, content=json.dumps(conversation).encode()), error_response(503)])

    def respond(request: httpx.Request) -> httpx.Response:
        return next(reads) if request.method == "GET" else error_response(502)

    requests: list[httpx.Request] = []
    context = AttemptContext()
    reply = ReplyToConversationInput(501, "Response to Jane")
    assert await run(respond, context, requests, input=reply) == "retry"
    context.advance()
    assert await run(respond, context, requests, input=reply) == "retry"
    assert context.marker is not None and context.marker.state == State.CLAIMED
    assert posts(requests) == 1
