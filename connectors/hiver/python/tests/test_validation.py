# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Validate Hiver fields at the Go/Python representation boundary."""

from datetime import UTC, datetime

import pytest

from dex_connectors.hiver import UpdateConversationInput
from dex_connectors.hiver.client import parse_go_rfc3339
from dex_connectors.hiver.operations import _validate_update


@pytest.mark.parametrize(
    ("value", "expected"),
    [
        ("2026-11-03T15:00:00,1234Z", datetime(2026, 11, 3, 15, 0, 0, 123400, UTC)),
        ("2026-11-03T15:00:00.123456+24:59", datetime(2026, 11, 2, 14, 1, 0, 123456, UTC)),
        ("2024-02-29T00:00:00Z", datetime(2024, 2, 29, tzinfo=UTC)),
    ],
)
def test_parse_go_rfc3339_exact_microseconds(value: str, expected: datetime) -> None:
    """Keep Go's accepted comma fractions, leap days, and large offsets exact."""
    assert parse_go_rfc3339(value) == expected


@pytest.mark.parametrize(
    "value",
    [
        "2026-02-30T00:00:00Z",
        "2025-02-29T00:00:00Z",
        "2026-11-03T24:00:00Z",
        "2026-11-03T15:00:00+25:00",
        "2026-11-03T15:00:00.123456001Z",
    ],
)
def test_parse_go_rfc3339_rejects_invalid_or_lossy_values(value: str) -> None:
    """Reject invalid dates and provider precision the Python codec cannot keep."""
    with pytest.raises(ValueError):
        parse_go_rfc3339(value)


@pytest.mark.parametrize("address", ["é@example.com", "agent@例え.com", "agent@[127.0.0.1]"])
def test_update_accepts_go_bare_address_forms(address: str) -> None:
    """Accept Go mail.ParseAddress's Unicode and domain literal forms."""
    _validate_update(UpdateConversationInput("105902", "573741352", assignee_email=address))


@pytest.mark.parametrize("address", ["a..b@example.com", "Agent <agent@example.com>", "a b@example.com"])
def test_update_rejects_non_bare_addresses(address: str) -> None:
    """Reject malformed or display-name address forms before a provider request."""
    with pytest.raises(ValueError, match="bare email address"):
        _validate_update(UpdateConversationInput("105902", "573741352", assignee_email=address))
