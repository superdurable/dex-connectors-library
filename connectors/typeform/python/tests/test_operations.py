# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused Typeform parsing and transport classification checks."""

from datetime import UTC, datetime

import dex_connectors.sdk as sdk
import httpx
import pytest

from dex_connectors.typeform import Client, Config, Credentials, GetFormInput
from dex_connectors.typeform.operations import _response, _timestamp


@pytest.mark.parametrize(
    "value",
    [
        "2026-02-30T12:00:00Z",
        "2025-02-29T12:00:00Z",
        "2026-01-01T24:00:00Z",
        "2026-01-01 12:00:00Z",
        "2026-01-01T12:00:00+01:60",
    ],
)
def test_timestamp_rejects_invalid_calendar_values(value: str) -> None:
    """Reject dates and clocks that Go time.Parse rejects."""
    with pytest.raises(ValueError):
        _timestamp(value)


def test_timestamp_accepts_comma_fraction_and_large_offset() -> None:
    """Match Go RFC3339Nano's comma fraction and large numeric offset."""
    assert _timestamp("2026-03-01T12:00:00,123+99:59") == datetime(2026, 2, 25, 8, 1, 0, 123000, UTC)
    assert _timestamp("0001-01-01T00:00:00Z") == datetime.min.replace(tzinfo=UTC)


@pytest.mark.parametrize(
    "malformed",
    [
        {"calculated": {"score": "high"}},
        {"variables": [{"key": "score", "type": "number", "number": "4"}]},
        {
            "answers": [
                {
                    "type": "choices",
                    "field": {"id": "f1", "type": "checkbox"},
                    "choices": {"ids": "not-a-list"},
                }
            ]
        },
    ],
)
def test_wrong_typed_provider_fields_are_invalid(malformed: dict[str, object]) -> None:
    """Reject values that Go's typed JSON decoder cannot read."""
    with pytest.raises(ValueError):
        _response({"token": "response1", **malformed})


@pytest.mark.asyncio
async def test_oversized_error_body_keeps_rate_limit_status() -> None:
    """Classify a 429 by status even when its body exceeds the success limit."""
    connection = sdk.ConnectionRef("typeform", "forms")
    credentials = sdk.StaticCredentialProvider(
        {connection: Credentials(access_token=sdk.SecretString("inert-token"))}
    )
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(429, content=b"x" * 2048))
    ) as transport:
        client = Client(Config(max_response_bytes=10), credentials, transport)
        call = sdk.Call(
            sdk.CallID("test-call"),
            connection,
            sdk.OperationRef("typeform", "getForm"),
            sdk.IdempotencyKey(""),
        )
        attempt = await client.get_form().invoke(call, GetFormInput(form_id="form123"))
        assert attempt.kind == "retry"
        assert attempt.failure is not None
        assert attempt.failure.kind == sdk.FailureKind.RATE_LIMIT
