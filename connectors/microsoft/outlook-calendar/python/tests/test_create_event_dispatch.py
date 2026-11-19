# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""createEvent never resends an accepted POST and retries a refused one whose refresh failed."""

import asyncio
from collections.abc import Awaitable, Callable
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    MutationResult,
    ReauthorizationRequiredError,
    RetryError,
    SecretString,
    run_mutation,
)
from dex_connectors.sdk.projectconfig.broker import CredentialBrokerUnavailableError
from dex_connectors.sdk.singledispatch import Marker

from dex_connectors.microsoft_outlook_calendar import Config, Credentials
from dex_connectors.microsoft_outlook_calendar._generated.definitions import CREATE_EVENT_DEFINITION
from dex_connectors.microsoft_outlook_calendar.client import Client
from dex_connectors.microsoft_outlook_calendar.models import (
    CreateEventInput,
    CreateEventOutput,
    EventDateTime,
)

CONNECTION = ConnectionRef("microsoft", "outlook-calendar-test")
CALL_ID = CallID("6d1d7c2e-9b44-5a4e-8f0b-3c2f4b1e7a10")
EVENT = {
    "@odata.etag": 'W/"etag-1"',
    "id": "evt-1",
    "subject": "Kickoff",
    "showAs": "busy",
    "type": "singleInstance",
    "start": {"dateTime": "2026-02-24T17:00:00", "timeZone": "UTC"},
    "end": {"dateTime": "2026-02-24T18:00:00", "timeZone": "UTC"},
    "originalStartTimeZone": "UTC",
    "originalEndTimeZone": "UTC",
}
INPUT = CreateEventInput(
    subject="Kickoff",
    start=EventDateTime("2026-02-24T09:00:00-08:00", "America/Los_Angeles"),
    end=EventDateTime("2026-02-24T10:00:00-08:00", "America/Los_Angeles"),
)


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None

    @property
    def marker(self) -> Marker | None:
        """Decode this call's marker from the last heartbeat."""
        value = self.heartbeat_value
        if isinstance(value, Marker):
            return value
        if not isinstance(value, dict):
            return None
        entry = value.get("singleDispatchMarkers", {}).get(str(CALL_ID))
        return None if entry is None else Marker.from_wire(entry)

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Return the last heartbeat, translating it to the requested format."""
        if isinstance(self.heartbeat_value, dict) and value_type is Marker:
            return self.marker
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value


class RefreshingCredentials:
    """Resolve a rejected token and fail the first forced refreshes with the given errors."""

    def __init__(self, refresh_errors: list[Exception]) -> None:
        self.refresh_errors = refresh_errors
        self.forced_refreshes = 0

    async def resolve(self, call: Call) -> Credentials:
        """Return the token Graph rejects."""
        return Credentials(access_token=SecretString("rejected-token"))

    async def resolve_after_rejection(self, call: Call, driver: object) -> Credentials:
        """Fail with the next configured error, then return a replacement token."""
        self.forced_refreshes += 1
        if self.forced_refreshes <= len(self.refresh_errors):
            raise self.refresh_errors[self.forced_refreshes - 1]
        return Credentials(access_token=SecretString("replacement-token"))


class StaticCredentials:
    """Always resolve one usable token."""

    async def resolve(self, call: Call) -> Credentials:
        """Return the token."""
        return Credentials(access_token=SecretString("calendar-token"))


class BrokenBodyStream(httpx.AsyncByteStream):
    """Yield part of a body, then fail as a connection lost mid-body does."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"id":"evt-1"'
        raise httpx.ReadError("connection reset mid-body")


class StalledBodyStream(httpx.AsyncByteStream):
    """Stall the body after headers arrive, past the Fence write window."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        await asyncio.sleep(30)
        yield b"{}"


Answer = Callable[[httpx.Request], Awaitable[httpx.Response]]


def lookup_answer(is_found: Callable[[], bool]) -> httpx.Response:
    """Answer a marker lookup with the event or an empty page."""
    return httpx.Response(200, json={"value": [EVENT] if is_found() else []})


async def run_attempts(
    answer: Answer, credentials: Any, context: AttemptContext, max_attempts: int
) -> tuple[MutationResult[CreateEventOutput] | RetryError, int]:
    """Run createEvent attempts until a branch is selected or max_attempts pass."""

    async def respond(request: httpx.Request) -> httpx.Response:
        return await answer(request)

    result: MutationResult[CreateEventOutput] | RetryError | None = None
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        client = Client(Config(), credentials, http_client)
        for attempt in range(1, max_attempts + 1):
            context.attempt = attempt
            call = Call(
                CALL_ID,
                CONNECTION,
                CREATE_EVENT_DEFINITION.operation,
                context=context,  # type: ignore[arg-type]
                max_sends=CREATE_EVENT_DEFINITION.max_sends,
            )
            try:
                return await run_mutation(client.create_event(), call, INPUT), attempt
            except RetryError as error:
                result = error
    assert result is not None
    return result, max_attempts


@pytest.mark.asyncio
async def test_a_broken_accepted_body_is_read_back_and_never_resent() -> None:
    posts: list[httpx.Request] = []

    async def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return lookup_answer(lambda: bool(posts))
        posts.append(request)
        return httpx.Response(201, stream=BrokenBodyStream())

    result, attempts = await run_attempts(answer, StaticCredentials(), AttemptContext(), 4)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert result.value.event.id == "evt-1"
    assert attempts == 1 and len(posts) == 1


@pytest.mark.asyncio
async def test_a_broken_accepted_body_whose_lookup_lags_selects_uncertain_without_a_resend() -> None:
    posts: list[httpx.Request] = []
    context = AttemptContext()

    async def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return lookup_answer(lambda: False)
        posts.append(request)
        return httpx.Response(201, stream=BrokenBodyStream())

    result, attempts = await run_attempts(answer, StaticCredentials(), context, 4)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert attempts == 2 and len(posts) == 1
    assert context.marker is not None and context.marker.provider_id == "accepted"


@pytest.mark.asyncio
async def test_the_write_deadline_cutting_an_accepted_body_is_never_resent() -> None:
    posts: list[httpx.Request] = []
    context = AttemptContext()
    # A short Step deadline keeps the Fence write window near one second.
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.5)

    async def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return lookup_answer(lambda: False)
        posts.append(request)
        return httpx.Response(201, stream=StalledBodyStream())

    result, _ = await run_attempts(answer, StaticCredentials(), context, 1)
    assert isinstance(result, RetryError)
    assert len(posts) == 1
    assert context.marker is not None and context.marker.provider_id == "accepted"


@pytest.mark.asyncio
async def test_the_write_deadline_before_headers_resends_below_the_cap() -> None:
    posts: list[httpx.Request] = []
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.5)

    async def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return lookup_answer(lambda: False)
        posts.append(request)
        await asyncio.sleep(30)
        return httpx.Response(201, json=EVENT)

    result, _ = await run_attempts(answer, StaticCredentials(), context, 1)
    assert isinstance(result, RetryError)
    assert len(posts) == 1
    assert context.marker is not None and context.marker.state == "claimed"
    assert not context.marker.provider_id, "no 2xx arrived, so a later attempt may resend"


@pytest.mark.asyncio
async def test_a_refused_post_whose_refresh_fails_transiently_does_not_count_against_the_cap() -> None:
    posts: list[httpx.Request] = []

    async def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return lookup_answer(lambda: False)
        posts.append(request)
        if request.headers["Authorization"] == "Bearer rejected-token":
            return httpx.Response(401, json={"error": {"code": "InvalidAuthenticationToken"}})
        return httpx.Response(201, json=EVENT)

    unavailable: list[Exception] = [CredentialBrokerUnavailableError() for _ in range(3)]
    result, attempts = await run_attempts(answer, RefreshingCredentials(unavailable), AttemptContext(), 6)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert attempts == 4 and len(posts) == 5


@pytest.mark.asyncio
async def test_a_refused_post_whose_refresh_is_terminal_selects_provider_rejected() -> None:
    posts: list[httpx.Request] = []

    async def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return lookup_answer(lambda: False)
        posts.append(request)
        return httpx.Response(401, json={"error": {"code": "InvalidAuthenticationToken"}})

    credentials = RefreshingCredentials([ReauthorizationRequiredError()])
    result, attempts = await run_attempts(answer, credentials, AttemptContext(), 3)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert attempts == 1 and len(posts) == 1
