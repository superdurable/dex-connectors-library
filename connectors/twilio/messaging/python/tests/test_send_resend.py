# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Twilio messages whose outcome is unknown."""

import json
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

from dex_connectors.twilio_messaging import Client, Config, Credentials
from dex_connectors.twilio_messaging._generated.definitions import SEND_MESSAGE_DEFINITION
from dex_connectors.twilio_messaging.models import Message, SendMessageInput

ACCOUNT = "AC" + "0123456789abcdef" * 2
MESSAGE_SID = "SM" + "fedcba9876543210" * 2
CONNECTION = ConnectionRef("twilio-messaging", "twilio-messaging")
CALL_ID = CallID("a29894ac-874e-5a65-a5a5-161c71200769")
SERVER_FAILURE = (500, {"code": 20500})
CREATED = (
    201,
    {
        "sid": MESSAGE_SID,
        "account_sid": ACCOUNT,
        "from": "+14155550100",
        "to": "+14155550123",
        "status": "queued",
    },
)


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


def sequence(*answers: tuple[int, object]) -> Answer:
    """Answer each request with the next response, repeating the last."""
    responses = list(answers)

    def answer(_: httpx.Request) -> tuple[int, object]:
        return responses.pop(0) if len(responses) > 1 else responses[0]

    return answer


async def send(
    answer: Answer,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = SEND_MESSAGE_DEFINITION.max_sends,
    auth_token: str = "auth-token-sentinel-0123456789ab",
) -> MutationResult[Message] | RetryError:
    """Run one Execute attempt of sendMessage, as the Mutation Step passes the definition cap."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        return httpx.Response(status, content=json.dumps(body).encode())

    credentials = StaticCredentialProvider({CONNECTION: Credentials(auth_token=SecretString(auth_token))})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config(account_sid=ACCOUNT, default_sender="+14155550100"), credentials, transport)
        call = Call(
            CALL_ID,
            CONNECTION,
            OperationRef("twilio-messaging", "sendMessage"),
            context=context,
            max_sends=max_sends,
        )
        try:
            message = SendMessageInput(to="+14155550123", body="hello")
            return await run_mutation(client.send_message(), call, message)
        except RetryError as error:
            return error


@pytest.mark.parametrize("status", [500, 502, 408, 409, 425])
async def test_unknown_outcome_resends_below_cap(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence((status, {}), CREATED)
    assert isinstance(await send(answer, context, requests), RetryError)
    assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = 2
    result = await send(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "accepted"
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


async def test_saved_message_sid_is_read_back_and_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await send(lambda _: CREATED, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "accepted"
    context.attempt = 2
    result = await send(lambda _: (200, CREATED[1]), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "accepted"
    assert [request.method for request in requests] == ["POST", "GET"]


@pytest.mark.parametrize("status", [400, 403, 404, 413, 422])
async def test_definite_refusal_is_never_resent(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await send(lambda _: (status, {"code": 21211}), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


async def test_refusal_of_a_resend_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence(SERVER_FAILURE, (400, {"code": 21211}))
    assert isinstance(await send(answer, context, requests), RetryError)
    context.attempt = 2
    result = await send(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 2


async def test_fail_closed_returns_retry() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.read_error = OSError("heartbeat unavailable")
    assert isinstance(await send(lambda _: CREATED, context, requests), RetryError)
    assert requests == []


async def test_credential_failure_after_possible_send_retries() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await send(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = 2
    result = await send(lambda _: SERVER_FAILURE, context, requests, auth_token="")
    assert isinstance(result, RetryError)
    assert len(requests) == 1
    result = await send(lambda _: SERVER_FAILURE, AttemptContext(), requests, auth_token="")
    assert isinstance(result, MutationResult) and result.branch == "defect"
