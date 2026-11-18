# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Telegram messages whose outcome is unknown."""

import asyncio
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    CredentialProvider,
    MutationResult,
    OperationRef,
    ReauthorizationRequiredError,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
)
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError
from dex_connectors.sdk.singledispatch import Marker

from dex_connectors.telegram import Client, Config, Credentials, SendMessageInput, SentMessage
from dex_connectors.telegram._generated.definitions import SEND_MESSAGE_DEFINITION

CONNECTION = ConnectionRef("telegram", "telegram")
CALL_ID = CallID("bbe66292-151f-5c40-bdfe-0553b4fb902f")
TOKEN = "123456:fake_test_token"
SENT_BODY = {"ok": True, "result": {"message_id": 52, "date": 1794110000, "chat": {"id": -1001234567890}}}
SERVER_FAILURE = (500, {"ok": False, "error_code": 500})


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None
        self.read_error: Exception | None = None

    @property
    def marker(self) -> Marker | None:
        """Decode this call's marker from a single-marker or per-call map heartbeat."""
        if isinstance(self.heartbeat_value, Marker):
            return self.heartbeat_value
        if not isinstance(self.heartbeat_value, dict):
            return None
        entry = self.heartbeat_value.get("singleDispatchMarkers", {}).get(str(CALL_ID))
        return None if entry is None else Marker.from_wire(entry)

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        if self.read_error is not None:
            raise self.read_error
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Return the last heartbeat, translating a seeded marker to the requested format."""
        if isinstance(self.heartbeat_value, Marker) and value_type is not Marker:
            marker = self.heartbeat_value
            return {"singleDispatchMarkers": {str(marker.call_id): marker.to_wire()}}
        if isinstance(self.heartbeat_value, dict) and value_type is Marker:
            return self.marker
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value


Answer = Callable[[httpx.Request], tuple[int, object]]


async def send(
    answer: Answer,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = SEND_MESSAGE_DEFINITION.max_sends,
    token: str = TOKEN,
    max_response_bytes: int = 1048576,
    credentials: CredentialProvider[Credentials] | None = None,
) -> MutationResult[SentMessage] | RetryError:
    """Run one Execute attempt of sendMessage, as the Mutation Step passes the definition cap."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        if isinstance(body, httpx.AsyncByteStream):
            return httpx.Response(status, stream=body)
        return httpx.Response(status, json=body)

    if credentials is None:
        credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        config = Config(base_url="http://127.0.0.1:12345", max_response_bytes=max_response_bytes)
        client = Client(config, credentials, transport)
        call = Call(
            CALL_ID, CONNECTION, OperationRef("telegram", "sendMessage"), context=context, max_sends=max_sends
        )
        try:
            message = SendMessageInput("@meridian_team", "hello")
            return await run_mutation(client.send_message(), call, message)
        except RetryError as error:
            return error


def sequence(*answers: tuple[int, object]) -> Answer:
    """Answer each request with the next response, repeating the last."""
    responses = list(answers)

    def answer(_: httpx.Request) -> tuple[int, object]:
        return responses.pop(0) if len(responses) > 1 else responses[0]

    return answer


@pytest.mark.parametrize("status", [500, 502, 408, 409, 425, 418])
async def test_unknown_outcome_resends_below_cap(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence((status, {"ok": False}), (200, SENT_BODY))
    assert isinstance(await send(answer, context, requests), RetryError)
    assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = 2
    result = await send(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "sent"
    assert context.marker is not None and context.marker.send_count == 2
    assert len(requests) == 2


async def test_unknown_outcome_selects_uncertain_at_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, SEND_MESSAGE_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await send(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = SEND_MESSAGE_DEFINITION.max_sends
    result = await send(lambda _: SERVER_FAILURE, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    context.attempt += 1
    result = await send(lambda _: SERVER_FAILURE, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 3


async def test_application_send_cap_of_one_never_resends() -> None:
    requests: list[httpx.Request] = []
    result = await send(lambda _: SERVER_FAILURE, AttemptContext(), requests, max_sends=1)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


class BrokenBodyStream(httpx.AsyncByteStream):
    """Send part of a response body, then drop the connection."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"ok":'
        raise httpx.ReadError("connection dropped")


@pytest.mark.parametrize(
    ("body", "max_response_bytes"),
    [
        ("not an envelope", 1048576),
        ({}, 1048576),
        ({"ok": True, "result": {"text": "x" * 200}}, 128),
        (None, 1048576),
    ],
)
async def test_accepted_unreadable_response_is_never_resent(body: object, max_response_bytes: int) -> None:
    requests: list[httpx.Request] = []
    payload: object = BrokenBodyStream() if body is None else body
    result = await send(
        lambda _: (200, payload), AttemptContext(), requests, max_response_bytes=max_response_bytes
    )
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_unlisted_error_reporting_acceptance_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    result = await send(lambda _: (409, SENT_BODY), AttemptContext(), requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


@pytest.mark.parametrize("status", [400, 401, 403, 404, 413, 422])
async def test_definite_refusal_is_never_resent(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await send(lambda _: (status, {"ok": False, "error_code": status}), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


async def test_refusal_of_a_resend_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence(SERVER_FAILURE, (400, {"ok": False, "error_code": 400}))
    assert isinstance(await send(answer, context, requests), RetryError)
    context.attempt = 2
    result = await send(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 2


async def test_accepted_but_incomplete_response_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    incomplete = (200, {"ok": True, "result": {"message_id": 52}})
    result = await send(lambda _: incomplete, AttemptContext(), requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_confirmed_message_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await send(lambda _: (200, SENT_BODY), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "sent"
    context.attempt = 2
    result = await send(lambda _: (200, SENT_BODY), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_fail_closed_returns_retry() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.read_error = OSError("heartbeat unavailable")
    assert isinstance(await send(lambda _: (200, SENT_BODY), context, requests), RetryError)
    assert requests == []


async def test_invalid_token_after_possible_send_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await send(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = 2
    result = await send(lambda _: SERVER_FAILURE, context, requests, token="bad")
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1
    result = await send(lambda _: SERVER_FAILURE, AttemptContext(), requests, token="bad")
    assert isinstance(result, MutationResult) and result.branch == "defect"


class FailingCredentialProvider:
    """Raise one fixed error for every credential read."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        raise self.error


@pytest.mark.parametrize(
    "error", [CredentialBrokerUnavailableError("broker down"), ValueError("connection is not configured")]
)
async def test_transient_credential_failure_before_send_retries(error: Exception) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await send(
        lambda _: (200, SENT_BODY), context, requests, credentials=FailingCredentialProvider(error)
    )
    assert isinstance(result, RetryError), "a transient credential failure returns Retry, not defect"
    assert context.marker is not None and context.marker.state == "notSent"
    context.attempt = 2
    result = await send(lambda _: (200, SENT_BODY), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "sent"
    assert context.marker is not None and context.marker.send_count == 1
    assert len(requests) == 1


async def test_transient_credential_failure_after_possible_send_keeps_the_claim() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await send(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = 2
    unavailable = FailingCredentialProvider(CredentialBrokerUnavailableError("broker down"))
    result = await send(lambda _: SERVER_FAILURE, context, requests, credentials=unavailable)
    assert isinstance(result, RetryError)
    assert context.marker is not None and context.marker.state == "claimed"
    assert len(requests) == 1


@pytest.mark.parametrize(
    "error",
    [
        ReauthorizationRequiredError("revoked"),
        ObjectNotFoundError("missing"),
        ConnectionNotSelectedError("unselected"),
        CredentialBrokerRejectedError("refused"),
    ],
)
async def test_terminal_credential_failure(error: Exception) -> None:
    requests: list[httpx.Request] = []
    terminal = FailingCredentialProvider(error)
    result = await send(lambda _: SERVER_FAILURE, AttemptContext(), requests, credentials=terminal)
    assert isinstance(result, MutationResult) and result.branch == "defect"
    context = AttemptContext()
    assert isinstance(await send(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = 2
    result = await send(lambda _: SERVER_FAILURE, context, requests, credentials=terminal)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


class StalledBodyStream(httpx.AsyncByteStream):
    """Send part of a response body, then stall past the Fence write window."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"ok":'
        await asyncio.sleep(5)
        yield b"true}"


async def test_write_window_cutting_an_accepted_body_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
    result = await send(lambda _: (200, StalledBodyStream()), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_write_window_cutting_an_error_body_resends_below_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
    result = await send(lambda _: (500, StalledBodyStream()), context, requests)
    assert isinstance(result, RetryError)
    assert context.marker is not None and context.marker.state == "claimed"
    assert len(requests) == 1
