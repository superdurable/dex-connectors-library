# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""createEvent never concludes a conflict about an event an earlier attempt may have inserted."""

from types import SimpleNamespace
from typing import Any, cast

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    IdempotencyKey,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.google_calendar import Client, Config, CreateEventInput, Credentials, EventDateTime

CONNECTION = ConnectionRef("google", "calendar")
INPUT = CreateEventInput(
    calendar_id="primary",
    event_id="event12345",
    summary="Planning",
    start=EventDateTime(date_time="2026-02-24T09:00:00-08:00", time_zone="America/Los_Angeles"),
    end=EventDateTime(date_time="2026-02-24T10:00:00-08:00", time_zone="America/Los_Angeles"),
)


def handle(request: httpx.Request) -> httpx.Response:
    """Report the event ID as taken while the event stays unreadable."""
    return httpx.Response(409 if request.method == "POST" else 404, content=b"{}")


@pytest.mark.asyncio
@pytest.mark.parametrize(("attempt", "expected_kind"), [(1, "branch"), (2, "retry")])
async def test_taken_unreadable_id_conflicts_only_on_a_first_attempt(
    attempt: int, expected_kind: str
) -> None:
    """A first attempt selects conflict; a later one retries, because the event may be its own."""
    call = Call(
        CallID("7a1c9c1e-5d8c-4f1b-9b8e-0c8c8f7f2a10"),
        CONNECTION,
        OperationRef("google-calendar", "createEvent"),
        IdempotencyKey("event12345"),
        cast(Any, SimpleNamespace(attempt=attempt)),
    )
    credentials = StaticCredentialProvider({CONNECTION: Credentials(access_token=SecretString("token"))})
    client = Client(Config(), credentials, httpx.AsyncClient(transport=httpx.MockTransport(handle)))
    result = await client.create_event().invoke(call, INPUT)
    assert result.kind == expected_kind
    if expected_kind == "branch":
        assert result.branch == "conflict"
