# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused provider boundary and timestamp tests beyond recorded scenarios."""

from datetime import UTC, datetime

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    OperationRef,
    ReauthorizationRequiredError,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.pipedrive import Client, Config, Credentials, UpsertObjectInput
from dex_connectors.pipedrive.models import decode_record_body, parse_go_rfc3339
from dex_connectors.pipedrive.operations import _upsert_bodies


class _FailingCredentials:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        raise self.error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "outcome", "kind"),
    [
        (ReauthorizationRequiredError(), "rejected", FailureKind.AUTHENTICATION),
        (OSError("temporary"), "retry", FailureKind.AVAILABILITY),
        (ValueError("connection is not configured"), "defect", FailureKind.AUTHENTICATION),
    ],
)
async def test_credential_resolution_routes_terminal_and_transient_errors(
    error: Exception, outcome: str, kind: FailureKind
) -> None:
    """Distinguish reconnect, temporary lookup, and missing connection failures."""
    connection = ConnectionRef("pipedrive", "unit")
    call = Call(CallID("test-call"), connection, OperationRef("pipedrive", "getObject"))
    client = Client(Config(endpoint="http://127.0.0.1"), _FailingCredentials(error))
    try:
        _, issue = await client.resolve(call, "getObject")
        assert issue is not None and issue.outcome == outcome
        assert issue.failure is not None and issue.failure.kind == kind
    finally:
        await client.aclose()


@pytest.mark.parametrize("value", ["2026-02-30T12:00:00Z", "2025-02-29T12:00:00Z", "2026-01-01T24:00:00Z"])
def test_invalid_go_timestamps(value: str) -> None:
    """Reject invalid calendar dates and an hour Go rejects."""
    with pytest.raises(ValueError):
        parse_go_rfc3339(value)


def test_go_provider_timestamp_keeps_nanos_and_large_offset() -> None:
    """Preserve Go's nanoseconds and accept its wide RFC 3339 offsets."""
    record = decode_record_body(
        "deals",
        b'{"success":true,"data":{"id":42,"add_time":"2026-11-05T12:00:00,123456789+99:59"}}',
    )
    assert record.add_time == "2026-11-01T08:01:00.123456789Z"


@pytest.mark.parametrize("number", ["12345678901234567.89", "1e-7", "1e400", "NaN"])
def test_record_rejects_json_numbers_that_cannot_round_trip(number: str) -> None:
    """Fail visibly instead of silently rounding an open-ended CRM field."""
    body = ('{"success":true,"data":{"id":42,"value":' + number + '}}').encode()
    with pytest.raises(ValueError, match="fields or custom_fields"):
        decode_record_body("deals", body)


@pytest.mark.asyncio
async def test_refresh_token_is_reused_by_later_requests_in_attempt() -> None:
    """A refreshed search token also authorizes the following upsert request."""
    class RefreshingCredentials:
        async def resolve(self, _call: Call) -> Credentials:
            return old

        async def resolve_after_rejection(self, _call: Call) -> Credentials:
            return new

    old = Credentials("pipedrive-oauth", access_token=SecretString("old"), api_domain="http://127.0.0.1")
    new = Credentials("pipedrive-oauth", access_token=SecretString("new"), api_domain="http://127.0.0.1")
    observed: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        observed.append(request.headers["Authorization"])
        return httpx.Response(401 if len(observed) == 1 else 200, json={"success": True, "data": []})

    connection = ConnectionRef("pipedrive", "unit")
    call = Call(CallID("test-call"), connection, OperationRef("pipedrive", "upsertObject"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as transport:
        client = Client(Config(endpoint="http://127.0.0.1"), RefreshingCredentials(), transport)
        resolved, issue = await client.resolve(call, "upsertObject")
        assert issue is None and resolved is not None
        await client.exchange(call, "upsertObject", resolved, "GET", "/api/v2/deals/search")
        await client.exchange(call, "upsertObject", resolved, "POST", "/api/v2/deals", body=b"{}")
    assert observed == ["Bearer old", "Bearer new", "Bearer new"]


@pytest.mark.parametrize(
    "address", ["ada@example.com", "ü@例子.测试", "foo@[127.0.0.1]", '"x y"@example.com']
)
def test_upsert_accepts_go_mail_addresses(address: str) -> None:
    """Accept plain RFC mailbox forms that Go mail.ParseAddress accepts."""
    identity, _, _ = _upsert_bodies(UpsertObjectInput("persons", "email", address, {"name": "Ada"}))
    assert identity == "email"


@pytest.mark.parametrize(
    "address", ["a..b@example.com", "Ada <ada@example.com>", "foo@example.com,bar@example.com"]
)
def test_upsert_rejects_non_plain_or_invalid_addresses(address: str) -> None:
    """Reject malformed or decorated identity values."""
    with pytest.raises(ValueError):
        _upsert_bodies(UpsertObjectInput("persons", "email", address, {"name": "Ada"}))


@pytest.mark.asyncio
async def test_oversized_error_body_keeps_rate_limit_status() -> None:
    """Classify 429 before discarding its oversized error body."""
    connection = ConnectionRef("pipedrive", "unit")
    credentials = StaticCredentialProvider(
        {connection: Credentials("api-token", SecretString("inert-token"))}
    )
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda request: httpx.Response(429, content=b"x" * 70000))
    ) as transport:
        client = Client(
            Config(endpoint="http://127.0.0.1"),
            credentials,
            transport,
            now=lambda: datetime.fromtimestamp(1, UTC),
        )
        call = Call(CallID("test-call"), connection, OperationRef("pipedrive", "getObject"))
        result = await client.exchange(
            call,
            "getObject",
            Credentials("api-token", SecretString("inert-token")),
            "GET",
            "/api/v2/deals/42",
        )
        assert result.outcome == "rateLimited"
        assert result.failure is not None and result.failure.kind == FailureKind.RATE_LIMIT
