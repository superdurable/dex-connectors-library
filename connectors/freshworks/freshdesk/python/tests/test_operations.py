# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Local Freshdesk validation and credential classification tests."""

from datetime import UTC, datetime

import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.freshworks_freshdesk import Client, Config, Credentials
from dex_connectors.freshworks_freshdesk.models import SearchTicketsInput
from dex_connectors.freshworks_freshdesk.operations import _filter, _timestamp, _validate_email


def test_client_rejects_invalid_helpdesk_domain() -> None:
    """Prevent a configuration value from selecting another provider host."""
    with pytest.raises(ValueError):
        Client(Config(domain="https://example.com"), StaticCredentialProvider({}))


def test_filter_requires_condition_and_rejects_duplicate_tags() -> None:
    """Catch invalid filters before credentials or provider requests are used."""
    with pytest.raises(ValueError):
        _filter(SearchTicketsInput())
    with pytest.raises(ValueError):
        _filter(SearchTicketsInput(tags=("Billing", "billing")))


@pytest.mark.parametrize("email", [".jane@example.com", "jane.@example.com", "jane..doe@example.com"])
def test_email_rejects_invalid_dot_atom(email: str) -> None:
    """Match Go's bare-address validation at the edge of the local part."""
    with pytest.raises(ValueError):
        _validate_email(email)


def test_email_accepts_go_mail_address_forms() -> None:
    """Preserve UTF-8 mailbox and IPv4 domain literal acceptance."""
    for email in ("tést@exämple.com", "jane@例子.测试", "jane@[127.0.0.1]"):
        _validate_email(email)


def test_timestamp_matches_go_calendar_and_large_offset_rules() -> None:
    """Reject impossible dates and accept Go's extended RFC 3339 offset range."""
    assert _timestamp("2026-03-01T12:00:00,5+99:59") == datetime(2026, 2, 25, 8, 1, 0, 500000, UTC)
    for value in ("2026-02-30T12:00:00Z", "2025-02-29T12:00:00Z", "2026-03-01T24:00:00Z"):
        with pytest.raises(ValueError):
            _timestamp(value)


@pytest.mark.asyncio
async def test_unsafe_api_key_is_terminal() -> None:
    """A malformed stored key needs reconnection and cannot be retried unchanged."""
    reference = ConnectionRef("freshdesk", "freshdesk-test")
    client = Client(
        Config(domain="acme"),
        StaticCredentialProvider({reference: Credentials(SecretString("unsafe:key"))}),
    )
    try:
        call = Call(
            CallID("703d7ec0-692e-50db-a914-86d5fecf7dcc"),
            reference,
            OperationRef("freshdesk", "searchTickets"),
        )
        key, failure, transient = await client.resolve_credentials(call, "searchTickets")
        assert key == ""
        assert failure is not None and failure.kind.value == "AUTHENTICATION"
        assert transient is False
    finally:
        await client.aclose()
