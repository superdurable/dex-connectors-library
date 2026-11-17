# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Calendly scheduling links whose outcome is unknown."""

from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.calendly as calendly
from dex_connectors.calendly._generated.definitions import CREATE_SCHEDULING_LINK_DEFINITION

CONNECTION = sdk.ConnectionRef("calendly", "scheduling")
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
        self.marker: Marker | None = None
        self.raw_marker: object = None

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.marker is not None or self.raw_marker is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last marker, or an untrusted raw value."""
        return self.marker if self.marker is not None else self.raw_marker

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's marker."""
        self.marker = value if isinstance(value, Marker) else None


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
    context.marker = Marker(CALL_ID, 1, "claimed", BOOKING_URL, 1)  # type: ignore[arg-type]
    result = await create_link(500, {}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "created"
    assert result.value is not None and result.value.booking_url == BOOKING_URL
    assert requests == []


@pytest.mark.asyncio
async def test_untrusted_checkpoint_returns_retry_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    context.raw_marker = {"callId": "foreign", "attempt": 1, "state": "claimed"}
    assert isinstance(await create_link(201, {}, context, requests), sdk.RetryError)
    assert requests == []
