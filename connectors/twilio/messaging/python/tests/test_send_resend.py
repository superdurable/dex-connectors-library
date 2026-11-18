# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Twilio messages whose outcome is unknown."""

import asyncio
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
    ReauthorizationRequiredError,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
)
from dex_connectors.sdk.projectconfig.broker import (
    CredentialBrokerUnavailableError,
    RejectedCredentialNotRefreshedError,
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


def sequence(*answers: tuple[int, object]) -> Answer:
    """Answer each request with the next response, repeating the last."""
    responses = list(answers)

    def answer(_: httpx.Request) -> tuple[int, object]:
        return responses.pop(0) if len(responses) > 1 else responses[0]

    return answer


class RereadingCredentialProvider(StaticCredentialProvider[Credentials]):
    """Resolve static credentials and reread them through ``refresh`` after a rejection."""

    def __init__(
        self, static: StaticCredentialProvider[Credentials], refresh: Callable[[], Credentials]
    ) -> None:
        super().__init__({})
        self.static = static
        self.refresh = refresh

    async def resolve(self, call: Call) -> Credentials:
        """Return the static credentials."""
        return await self.static.resolve(call)

    async def resolve_after_rejection(self, call: Call, driver: object) -> Credentials:
        """Return the reread credentials, or raise the configured refresh failure."""
        return self.refresh()


async def send(
    answer: Answer,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = SEND_MESSAGE_DEFINITION.max_sends,
    auth_token: str = "auth-token-sentinel-0123456789ab",
    max_response_bytes: int = 1048576,
    refresh: Callable[[], Credentials] | None = None,
) -> MutationResult[Message] | RetryError:
    """Run one Execute attempt of sendMessage, as the Mutation Step passes the definition cap.

    ``refresh`` makes the credential source reread credentials after a rejection by calling it.
    """

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        if isinstance(body, httpx.AsyncByteStream):
            return httpx.Response(status, stream=body)
        if isinstance(body, bytes):
            return httpx.Response(status, content=body)
        return httpx.Response(status, content=json.dumps(body).encode())

    credentials: StaticCredentialProvider[Credentials] = StaticCredentialProvider(
        {CONNECTION: Credentials(auth_token=SecretString(auth_token))}
    )
    if refresh is not None:
        credentials = RereadingCredentialProvider(credentials, refresh)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        config = Config(
            account_sid=ACCOUNT, default_sender="+14155550100", max_response_bytes=max_response_bytes
        )
        client = Client(config, credentials, transport)
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


class BrokenBodyStream(httpx.AsyncByteStream):
    """Send part of a response body, then drop the connection."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"sid":'
        raise httpx.ReadError("connection dropped")


@pytest.mark.parametrize(
    ("body", "max_response_bytes"),
    [(b'{"status":"queued"}', 1048576), (b'{"sid":"not-a-sid"}', 1048576), (None, 1048576), (None, 16)],
)
async def test_accepted_unusable_response_is_never_resent(
    body: bytes | None, max_response_bytes: int
) -> None:
    requests: list[httpx.Request] = []
    payload: object = json.dumps(CREATED[1]).encode() if max_response_bytes == 16 else body
    if payload is None:
        payload = BrokenBodyStream()
    result = await send(
        lambda _: (201, payload), AttemptContext(), requests, max_response_bytes=max_response_bytes
    )
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_error_status_naming_a_created_message_is_read_back_not_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def answer(request: httpx.Request) -> tuple[int, object]:
        return (409, CREATED[1]) if request.method == "POST" else (200, CREATED[1])

    result = await send(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "accepted"
    assert context.marker is not None and context.marker.provider_id == MESSAGE_SID
    assert [request.method for request in requests] == ["POST", "GET"]


class ProviderIDRefusingContext(AttemptContext):
    """Refuse the heartbeat that saves the message SID."""

    async def heartbeat(self, value: object = None) -> None:
        """Fail the provider ID checkpoint and keep every other heartbeat."""
        if MESSAGE_SID in json.dumps(value, default=str):
            raise RuntimeError("heartbeat refused")
        await super().heartbeat(value)


async def test_unsaved_named_sid_with_unavailable_read_back_selects_uncertain() -> None:
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> tuple[int, object]:
        return (409, CREATED[1]) if request.method == "POST" else (503, {})

    result = await send(answer, ProviderIDRefusingContext(), requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert [request.method for request in requests] == ["POST", "GET"]


async def test_saved_named_sid_read_back_retries_without_resending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def answer(request: httpx.Request) -> tuple[int, object]:
        if request.method == "POST":
            return (409, CREATED[1])
        return (503, {}) if len(requests) < 3 else (200, CREATED[1])

    assert isinstance(await send(answer, context, requests), RetryError)
    context.attempt = 2
    result = await send(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "accepted"
    assert [request.method for request in requests] == ["POST", "GET", "GET"]


async def test_oversized_refusal_body_is_an_unknown_outcome() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    oversized = b'{"code":21211,"message":"' + b"x" * 65536 + b'"}'
    answer = sequence((400, oversized), CREATED)
    assert isinstance(await send(answer, context, requests), RetryError)
    assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = 2
    result = await send(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "accepted"
    assert len(requests) == 2


async def test_fail_closed_returns_retry() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.read_error = OSError("heartbeat unavailable")
    assert isinstance(await send(lambda _: CREATED, context, requests), RetryError)
    assert requests == []


async def test_terminal_credential_failure_after_possible_send_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await send(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = 2
    result = await send(lambda _: SERVER_FAILURE, context, requests, auth_token="")
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1
    result = await send(lambda _: SERVER_FAILURE, AttemptContext(), requests, auth_token="")
    assert isinstance(result, MutationResult) and result.branch == "defect"


async def test_accepted_sid_whose_message_does_not_decode_is_read_back_not_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def answer(request: httpx.Request) -> tuple[int, object]:
        if request.method == "POST":
            return (201, {**CREATED[1], "status": "Not A Status"})
        return (200, CREATED[1])

    result = await send(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "accepted"
    assert result.value.sid == MESSAGE_SID
    assert context.marker is not None and context.marker.provider_id == MESSAGE_SID
    assert [request.method for request in requests] == ["POST", "GET"]


@pytest.mark.parametrize("status", [201, 409])
async def test_named_sid_that_contains_the_credential_is_never_saved_or_read_back(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    hex_auth_token = "0123456789abcdef" * 2
    reflected = {**CREATED[1], "sid": "SM" + hex_auth_token}
    result = await send(lambda _: (status, reflected), context, requests, auth_token=hex_auth_token)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert hex_auth_token not in result.receipt.provider_object_id
    assert hex_auth_token not in json.dumps(context.heartbeat_value, default=str)
    assert len(requests) == 1


UNCHANGED_TOKEN = "auth-token-sentinel-0123456789ab"


def _raise(error: Exception) -> Callable[[], Credentials]:
    def refresh() -> Credentials:
        raise error

    return refresh


@pytest.mark.parametrize(
    ("refresh", "expected"),
    [
        (_raise(CredentialBrokerUnavailableError("broker down")), "retry"),
        (_raise(TimeoutError()), "retry"),
        (lambda: Credentials(auth_token=SecretString("rotated-token-0123456789abcdef")), "retry"),
        (_raise(ReauthorizationRequiredError("revoked")), "providerRejected"),
        (_raise(RejectedCredentialNotRefreshedError("not refreshed")), "providerRejected"),
        (lambda: Credentials(auth_token=SecretString(UNCHANGED_TOKEN)), "providerRejected"),
    ],
    ids=["broker", "timeout", "rotated", "reauthorize", "not-refreshed", "unchanged"],
)
async def test_readable_unauthorized_retries_only_a_new_or_transiently_unavailable_credential(
    refresh: Callable[[], Credentials], expected: str
) -> None:
    """A readable 401 proves nothing was created, so a resendable outcome records canResend."""
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await send(lambda _: (401, {"code": 20003}), context, requests, refresh=refresh)
    assert len(requests) == 1
    assert context.marker is not None and context.marker.state == "canResend"
    if expected == "retry":
        assert isinstance(result, RetryError)
    else:
        assert isinstance(result, MutationResult) and result.branch == "providerRejected"


@pytest.mark.parametrize("unreadable", ["oversized", "interrupted"])
async def test_unreadable_unauthorized_body_is_an_unknown_outcome(unreadable: str) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    first: tuple[int, object] = (
        (401, b'{"code":20003,"message":"' + b"x" * 65536 + b'"}')
        if unreadable == "oversized"
        else (401, BrokenBodyStream())
    )
    answer = sequence(first, (401, {"code": 20003}))
    assert isinstance(await send(answer, context, requests), RetryError)
    assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = 2
    result = await send(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 2


class StalledBodyStream(httpx.AsyncByteStream):
    """Stall the response body past the Fence write window, then drop the connection."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        await asyncio.sleep(2)
        yield b"{"
        raise httpx.ReadError("connection dropped")


async def test_success_headers_then_write_deadline_cutting_the_body_are_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    # A short Step deadline shortens the Fence write window to about 0.7 seconds.
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
    result = await send(lambda _: (201, StalledBodyStream()), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1
