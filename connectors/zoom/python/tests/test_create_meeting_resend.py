# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Zoom meeting creates whose outcome is unknown."""

import asyncio
from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    MutationResult,
    OperationRef,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
)
from dex_connectors.sdk.singledispatch import Marker, State

import dex_connectors.zoom as zoom
from dex_connectors.zoom._generated.definitions import CREATE_MEETING_DEFINITION

CONNECTION = ConnectionRef("zoom", "zoom-test")
CALL_ID = CallID("d095d4cd-8f7d-53db-926e-50307d935d1f")
CREATED = (
    201,
    {
        "id": 92674392836,
        "topic": "Consultation",
        "type": 2,
        "start_time": "2036-02-24T17:00:00Z",
        "duration": 30,
        "timezone": "America/Los_Angeles",
    },
)
SERVER_FAILURE = (500, {"code": 500, "message": "Unknown"})
INPUT = zoom.CreateMeetingInput(
    topic="Consultation",
    start_time="2036-02-24T09:00:00-08:00",
    time_zone="America/Los_Angeles",
    duration_minutes=30,
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


async def create(
    answer: Answer,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = CREATE_MEETING_DEFINITION.max_sends,
    has_credentials: bool = True,
    access_token: str = "zoom-test-access-token",
    max_response_bytes: int = 1048576,
) -> MutationResult[zoom.Meeting] | RetryError:
    """Run one Execute attempt of createMeeting, as the Mutation Step passes the definition cap."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        if isinstance(body, httpx.AsyncByteStream):
            return httpx.Response(status, stream=body)
        if isinstance(body, bytes):
            return httpx.Response(status, content=body)
        return httpx.Response(status, json=body)

    credentials = zoom.Credentials(
        "zoom-client-id",
        SecretString("zoom-client-secret"),
        SecretString(access_token),
        SecretString("zoom-refresh-token"),
    )
    provider = StaticCredentialProvider({CONNECTION: credentials} if has_credentials else {})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        config = zoom.Config(endpoint="https://zoom.example/v2", max_response_bytes=max_response_bytes)
        client = zoom.Client(config, provider, transport)
        call = Call(
            CALL_ID, CONNECTION, OperationRef("zoom", "createMeeting"), context=context, max_sends=max_sends
        )
        try:
            return await run_mutation(client.create_meeting(), call, INPUT)
        except RetryError as error:
            return error


@pytest.mark.parametrize("status", [500, 502, 408, 409, 425, 418, 307])
async def test_unknown_outcome_resends_below_cap(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence((status, {"code": status}), CREATED)
    assert isinstance(await create(answer, context, requests), RetryError)
    assert context.marker is not None and context.marker.state == State.CLAIMED
    context.attempt = 2
    result = await create(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert context.marker is not None and context.marker.send_count == 2
    assert len(requests) == 2


async def test_unknown_outcome_selects_uncertain_at_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, CREATE_MEETING_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await create(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = CREATE_MEETING_DEFINITION.max_sends
    result = await create(lambda _: SERVER_FAILURE, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    context.attempt += 1
    result = await create(lambda _: SERVER_FAILURE, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 3


async def test_application_send_cap_of_one_never_resends() -> None:
    requests: list[httpx.Request] = []
    result = await create(lambda _: SERVER_FAILURE, AttemptContext(), requests, max_sends=1)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_saved_meeting_id_is_read_back_instead_of_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await create(lambda _: CREATED, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"
    context.attempt = 2
    result = await create(lambda _: (200, CREATED[1]), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert [request.method for request in requests] == ["POST", "GET"]


@pytest.mark.parametrize("status", [400, 403, 404, 422])
async def test_definite_refusal_is_never_resent(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await create(lambda _: (status, {"code": 300}), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert context.marker is not None and context.marker.state == State.CAN_RESEND
    assert len(requests) == 1


async def test_refusal_of_a_resend_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence(SERVER_FAILURE, (400, {"code": 300}))
    assert isinstance(await create(answer, context, requests), RetryError)
    context.attempt = 2
    result = await create(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 2


async def test_accepted_id_with_unusable_body_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    accepted = (201, {"id": 92674392836, "start_time": "not a time"})
    result = await create(lambda _: accepted, AttemptContext(), requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


class BrokenBodyStream(httpx.AsyncByteStream):
    """Send part of a response body, then drop the connection."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"id":926'
        raise httpx.ReadError("connection dropped")


@pytest.mark.parametrize(
    ("body", "max_response_bytes"),
    [
        (b'{"id":"not-a-number"}', 1048576),
        (b"{}", 1048576),
        (b'{"agenda":"' + b"x" * 4096 + b'"}', 2048),
        (None, 1048576),
    ],
)
async def test_accepted_unusable_response_is_never_resent(
    body: bytes | None, max_response_bytes: int
) -> None:
    requests: list[httpx.Request] = []
    payload: object = BrokenBodyStream() if body is None else body
    result = await create(
        lambda _: (201, payload), AttemptContext(), requests, max_response_bytes=max_response_bytes
    )
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_client_error_with_created_id_is_read_back_not_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def answer(request: httpx.Request) -> tuple[int, object]:
        return (425, {"id": 92674392836}) if request.method == "POST" else (200, CREATED[1])

    result = await create(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert context.marker is not None and context.marker.provider_id == "92674392836"
    assert [request.method for request in requests] == ["POST", "GET"]


class ProviderIDRefusingContext(AttemptContext):
    """Refuse the heartbeat that saves the meeting ID."""

    async def heartbeat(self, value: object = None) -> None:
        """Fail the provider ID checkpoint and keep every other heartbeat."""
        if "92674392836" in repr(value):
            raise RuntimeError("heartbeat refused")
        await super().heartbeat(value)


async def test_unsaved_created_id_with_unavailable_read_back_selects_uncertain() -> None:
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> tuple[int, object]:
        return (425, {"id": 92674392836}) if request.method == "POST" else (503, {"code": 503})

    result = await create(answer, ProviderIDRefusingContext(), requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert [request.method for request in requests] == ["POST", "GET"]


async def test_saved_created_id_read_back_retries_without_resending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def answer(request: httpx.Request) -> tuple[int, object]:
        if request.method == "POST":
            return (425, {"id": 92674392836})
        return (503, {"code": 503}) if len(requests) < 3 else (200, CREATED[1])

    assert isinstance(await create(answer, context, requests), RetryError)
    context.attempt = 2
    result = await create(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert [request.method for request in requests] == ["POST", "GET", "GET"]


async def test_unlisted_client_error_retries_with_an_unknown_outcome_kind() -> None:
    requests: list[httpx.Request] = []
    result = await create(lambda _: (425, {"code": 425}), AttemptContext(), requests)
    assert isinstance(result, RetryError)
    assert result.failure.kind == FailureKind.AVAILABILITY


async def test_oversized_refusal_body_is_an_unknown_outcome() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    oversized = b'{"code":300,"message":"' + b"x" * 65536 + b'"}'
    answer = sequence((400, oversized), CREATED)
    assert isinstance(await create(answer, context, requests), RetryError)
    assert context.marker is not None and context.marker.state == State.CLAIMED
    context.attempt = 2
    result = await create(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert len(requests) == 2


async def test_invalid_credentials_after_possible_send_select_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await create(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = 2
    result = await create(lambda _: SERVER_FAILURE, context, requests, access_token="")
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_fail_closed_returns_retry() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.read_error = OSError("heartbeat unavailable")
    assert isinstance(await create(lambda _: CREATED, context, requests), RetryError)
    assert requests == []


async def test_credential_failure_after_possible_send_retries() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await create(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = 2
    result = await create(lambda _: SERVER_FAILURE, context, requests, has_credentials=False)
    assert isinstance(result, RetryError)
    assert len(requests) == 1


@pytest.mark.parametrize("unreadable", ["oversized", "interrupted"])
async def test_unreadable_unauthorized_body_is_an_unknown_outcome(unreadable: str) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    first: tuple[int, object] = (
        (401, b'{"code":124,"message":"' + b"x" * 65536 + b'"}')
        if unreadable == "oversized"
        else (401, BrokenBodyStream())
    )
    answer = sequence(first, (401, {"code": 124, "message": "Invalid access token"}))
    assert isinstance(await create(answer, context, requests), RetryError)
    assert context.marker is not None and context.marker.state == State.CLAIMED
    context.attempt = 2
    result = await create(answer, context, requests)
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
    result = await create(lambda _: (201, StalledBodyStream()), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1
