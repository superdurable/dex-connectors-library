# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Telegram messages whose outcome is unknown."""

from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    MutationResult,
    OperationRef,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
)
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
        self.marker: Marker | None = None
        self.read_error: Exception | None = None

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        if self.read_error is not None:
            raise self.read_error
        return self.marker is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last marker."""
        return self.marker

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's marker."""
        self.marker = value if isinstance(value, Marker) else None


Answer = Callable[[httpx.Request], tuple[int, object]]


async def send(
    answer: Answer,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = SEND_MESSAGE_DEFINITION.max_sends,
    token: str = TOKEN,
) -> MutationResult[SentMessage] | RetryError:
    """Run one Execute attempt of sendMessage, as the Mutation Step passes the definition cap."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        return httpx.Response(status, json=body)

    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config(base_url="http://127.0.0.1:12345"), credentials, transport)
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


async def test_malformed_success_resends_below_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence((200, "not an envelope"), (200, SENT_BODY))
    assert isinstance(await send(answer, context, requests), RetryError)
    context.attempt = 2
    result = await send(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "sent"


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


async def test_credential_failure_after_possible_send_retries() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await send(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = 2
    assert isinstance(await send(lambda _: SERVER_FAILURE, context, requests, token="bad"), RetryError)
    assert len(requests) == 1
    result = await send(lambda _: SERVER_FAILURE, AttemptContext(), requests, token="bad")
    assert isinstance(result, MutationResult) and result.branch == "defect"
