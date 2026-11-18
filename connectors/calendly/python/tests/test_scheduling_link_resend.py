# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Calendly scheduling links whose outcome is unknown."""

import asyncio
from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)
from dex_connectors.sdk.singledispatch import Marker, State

import dex_connectors.calendly as calendly
from dex_connectors.calendly._generated.connection import (
    CredentialMaterialError,
    decode_credentials,
)
from dex_connectors.calendly._generated.definitions import CREATE_SCHEDULING_LINK_DEFINITION

CONNECTION = sdk.ConnectionRef("calendly", "scheduling")


def sdk_decoding_failure() -> ValueError:
    """Return the SDK project provider's error after the generated decoder rejects malformed material."""
    try:
        try:
            decode_credentials({"unknown_field": "value"})
        except ValueError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
    raise AssertionError("malformed material decoded")
CALL_ID = sdk.CallID("345b8fc6-bc68-513b-af1b-4f197c2e59f6")
EVENT_TYPE = "https://api.calendly.com/event_types/TYPE0001"
BOOKING_URL = "https://calendly.com/d/abcd-1234/30-minute-meeting"
MAX_SENDS = CREATE_SCHEDULING_LINK_DEFINITION.max_sends


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None
        self.heartbeat_read_error: Exception | None = None

    @property
    def marker(self) -> Marker | None:
        """Decode this call's checkpoint from either SDK heartbeat format."""
        if isinstance(self.heartbeat_value, Marker) or self.heartbeat_value is None:
            return self.heartbeat_value
        assert isinstance(self.heartbeat_value, dict)
        entry = self.heartbeat_value["singleDispatchMarkers"].get(str(CALL_ID))
        return None if entry is None else Marker.from_wire(entry)

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None or self.heartbeat_read_error is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat as the reading SDK expects it."""
        if self.heartbeat_read_error is not None:
            raise self.heartbeat_read_error
        seeded = self.heartbeat_value
        if isinstance(seeded, Marker) and value_type is not Marker:
            return {"singleDispatchMarkers": {str(seeded.call_id): seeded.to_wire()}}
        if isinstance(seeded, dict) and value_type is Marker:
            return self.marker
        return seeded

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value


async def create_link(
    status: int, body: dict[str, object], context: AttemptContext, requests: list[httpx.Request]
) -> sdk.MutationResult[calendly.SchedulingLink] | sdk.RetryError:
    """Run one Execute attempt of createSchedulingLink with the generated send cap."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(status, json=body)

    credentials = sdk.StaticCredentialProvider(
        {CONNECTION: calendly.Credentials(access_token=sdk.SecretString("inert-token"))}
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = calendly.Client(calendly.Config(), credentials, transport)
        call = sdk.Call(
            CALL_ID,
            CONNECTION,
            sdk.OperationRef("calendly", "createSchedulingLink"),
            context=context,
            max_sends=MAX_SENDS,
        )
        try:
            return await sdk.run_mutation(
                client.create_scheduling_link(), call, calendly.CreateSchedulingLinkInput(EVENT_TYPE)
            )
        except sdk.RetryError as error:
            return error


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [500, 409])
async def test_unknown_outcome_resends_until_the_send_cap(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, MAX_SENDS):
        context.attempt = attempt
        assert isinstance(await create_link(status, {}, context, requests), sdk.RetryError)
        assert context.marker is not None and context.marker.send_count == attempt
    context.attempt = MAX_SENDS
    result = await create_link(status, {}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    context.attempt += 1
    result = await create_link(status, {}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert len(requests) == MAX_SENDS, "no send is allowed at the cap"


@pytest.mark.asyncio
async def test_definite_refusal_of_first_send_records_can_resend() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await create_link(400, {"title": "Invalid Argument"}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"


@pytest.mark.asyncio
async def test_refusal_after_a_possible_send_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await create_link(502, {}, context, requests), sdk.RetryError)
    context.attempt = 2
    result = await create_link(400, {}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_saved_booking_url_selects_created_without_another_post() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    context.heartbeat_value = Marker(CALL_ID, 1, State.CLAIMED, BOOKING_URL, 1)
    result = await create_link(500, {}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "created"
    assert result.value is not None and result.value.booking_url == BOOKING_URL
    assert requests == []


class _SlowAcceptedBody(httpx.AsyncByteStream):
    """Send 2xx headers, then hold the body past the write window."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        await asyncio.sleep(30)
        yield b"{}"


class _BrokenBody(httpx.AsyncByteStream):
    """Break the response body after its headers arrived."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        raise httpx.ReadError("connection closed")
        yield b""


async def run_link(
    respond: Callable[[httpx.Request], httpx.Response], context: AttemptContext
) -> sdk.MutationResult[calendly.SchedulingLink] | sdk.RetryError:
    """Run one Execute attempt of createSchedulingLink against a custom provider answer."""
    credentials = sdk.StaticCredentialProvider(
        {CONNECTION: calendly.Credentials(access_token=sdk.SecretString("inert-token"))}
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = calendly.Client(calendly.Config(), credentials, transport)
        call = sdk.Call(
            CALL_ID,
            CONNECTION,
            sdk.OperationRef("calendly", "createSchedulingLink"),
            context=context,
            max_sends=MAX_SENDS,
        )
        try:
            return await sdk.run_mutation(
                client.create_scheduling_link(), call, calendly.CreateSchedulingLinkInput(EVENT_TYPE)
            )
        except sdk.RetryError as error:
            return error


@pytest.mark.asyncio
async def test_write_deadline_after_accepted_headers_is_never_resent() -> None:
    """A 5/6 write window of a 3-second budget ends while the accepted body is still arriving."""
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=3)

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(201, stream=_SlowAcceptedBody())

    result = await run_link(respond, context)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize("body", ["oversized", "broken"])
async def test_refusal_with_incomplete_body_resends_until_the_send_cap(body: str) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if body == "oversized":
            return httpx.Response(400, content=b"x" * (64 * 1024 + 1))
        return httpx.Response(400, stream=_BrokenBody())

    for attempt in range(1, MAX_SENDS):
        context.attempt = attempt
        assert isinstance(await run_link(respond, context), sdk.RetryError)
        assert context.marker is not None and context.marker.state != "canResend"
    context.attempt = MAX_SENDS
    result = await run_link(respond, context)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert len(requests) == MAX_SENDS


class _FailingCredentials:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> calendly.Credentials:
        raise self.error


async def create_link_with_failing_credentials(
    error: Exception, context: AttemptContext
) -> sdk.MutationResult[calendly.SchedulingLink] | sdk.RetryError:
    """Run one Execute attempt whose credential read raises error."""

    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request may be sent")

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = calendly.Client(calendly.Config(), _FailingCredentials(error), transport)
        call = sdk.Call(
            CALL_ID,
            CONNECTION,
            sdk.OperationRef("calendly", "createSchedulingLink"),
            context=context,
            max_sends=MAX_SENDS,
        )
        try:
            return await sdk.run_mutation(
                client.create_scheduling_link(), call, calendly.CreateSchedulingLinkInput(EVENT_TYPE)
            )
        except sdk.RetryError as error:
            return error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "is_terminal"),
    [
        (sdk.ReauthorizationRequiredError(), True),
        (ObjectNotFoundError(), True),
        (ConnectionNotSelectedError(), True),
        (CredentialBrokerRejectedError(), True),
        (CredentialMaterialError("Calendly stored credential does not match"), True),
        (sdk_decoding_failure(), True),
        (CredentialBrokerUnavailableError("credential broker is unavailable"), False),
        (ValueError("connection is not configured"), False),
        (RuntimeError("credential store hiccup"), False),
    ],
)
async def test_credential_failures_are_classified_before_and_after_a_possible_send(
    error: Exception, is_terminal: bool
) -> None:
    first_send = AttemptContext()
    result = await create_link_with_failing_credentials(error, first_send)
    if is_terminal:
        assert isinstance(result, sdk.MutationResult) and result.branch == "defect"
    else:
        assert isinstance(result, sdk.RetryError)
    assert first_send.marker is not None and first_send.marker.state == "notSent"

    requests: list[httpx.Request] = []
    after_send = AttemptContext()
    assert isinstance(await create_link(502, {}, after_send, requests), sdk.RetryError)
    after_send.attempt = 2
    result = await create_link_with_failing_credentials(error, after_send)
    if is_terminal:
        assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    else:
        assert isinstance(result, sdk.RetryError)
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_unreadable_checkpoint_returns_retry_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    context.heartbeat_read_error = RuntimeError("heartbeat store unavailable")
    assert isinstance(await create_link(201, {}, context, requests), sdk.RetryError)
    assert requests == []
