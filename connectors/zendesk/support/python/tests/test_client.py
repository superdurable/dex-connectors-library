# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zendesk Support parsing, credentials, and response classification edge cases."""

from datetime import UTC, datetime

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.zendesk_support import Client, Config, Credentials
from dex_connectors.zendesk_support.client import bare_email, decode_ticket, parse_time


@pytest.mark.parametrize(
    "value",
    ["a@example.com", "a+triage@localhost", "üser@éxample.com", "a@[127.0.0.1]", "a@[::1]"],
)
def test_bare_email_accepts_go_mailbox_forms(value: str) -> None:
    assert bare_email(value)


@pytest.mark.parametrize(
    "value",
    ["a..b@example.com", "a@example..com", "a@example.com.", "A <a@example.com>", "a@[999.999.999.999]"],
)
def test_bare_email_rejects_invalid_or_decorated_forms(value: str) -> None:
    assert not bare_email(value)


@pytest.mark.parametrize(
    "value",
    ["2026-02-30T10:00:00Z", "2026-02-29T10:00:00Z", "2026-01-01T24:00:00Z", "2026-01-01T00:00:00+03:60"],
)
def test_rfc3339_rejects_invalid_calendar_or_clock(value: str) -> None:
    with pytest.raises(ValueError):
        parse_time(value)


def test_rfc3339_accepts_go_large_offset_and_comma_fraction() -> None:
    assert parse_time("2026-01-01T00:00:00+99:59") == datetime(2025, 12, 27, 20, 1, tzinfo=UTC)
    assert parse_time("2024-02-29T10:00:00,25Z") == datetime(2024, 2, 29, 10, 0, 0, 250000, UTC)


def test_ticket_rejects_wrong_typed_provider_field() -> None:
    with pytest.raises(ValueError, match="id is not an integer"):
        decode_ticket(
            {
                "id": "35436",
                "status": "open",
                "created_at": "2026-01-01T00:00:00Z",
                "updated_at": "2026-01-01T00:00:00Z",
            },
            "https://acme.zendesk.com/agent/tickets/",
        )


async def test_large_error_body_still_classifies_by_status() -> None:
    reference = ConnectionRef("zendesk-support", "desk")
    credentials = Credentials("agent@example.com", SecretString("inert-token"))
    transport = httpx.MockTransport(lambda request: httpx.Response(429, content=b"x" * 500))
    async with httpx.AsyncClient(transport=transport) as http_client:
        client = Client(
            Config("acme", max_response_bytes=10),
            StaticCredentialProvider({reference: credentials}),
            http_client,
            api_base_url="http://127.0.0.1:37001/api/v2",
        )
        call = Call(
            CallID("call"), reference, OperationRef("zendesk-support", "searchTickets"), IdempotencyKey("")
        )
        exchange = await client.exchange(call, credentials, "searchTickets", "GET", "/search/export")
        assert exchange.outcome == "retry"
        assert exchange.failure is not None and exchange.failure.kind == FailureKind.RATE_LIMIT
