# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Calendly input and response rules beyond recorded provider scenarios."""

from datetime import UTC, datetime

import pytest

from dex_connectors.calendly.operations import _email, _event, _timestamp


@pytest.mark.parametrize(
    ("address", "accepted"),
    [
        ("ada@example.com", True),
        ("é@例子.中国", True),
        ("ada@[127.0.0.1]", True),
        ("ada@[::1]", True),
        ("ada@bad..com", False),
        ("a..b@example.com", False),
        ("Ada <ada@example.com>", False),
        ("ada@[IPv6:::1]", False),
    ],
)
def test_plain_address_matches_go_cases(address: str, accepted: bool) -> None:
    """Accept Unicode and literals while rejecting malformed dot atoms."""
    assert _email(address) is accepted


@pytest.mark.parametrize("value", ["2026-02-30T00:00:00Z", "2025-02-29T00:00:00Z", "2026-11-01T24:00:00Z"])
def test_invalid_calendar_timestamp(value: str) -> None:
    """Reject calendar values that Python's loose parsers might roll forward."""
    with pytest.raises(ValueError):
        _timestamp(value)


def test_timestamp_rejects_unrepresentable_nanoseconds() -> None:
    with pytest.raises(ValueError, match="exactly"):
        _timestamp("2026-11-01T12:00:00.123456789Z")


def test_go_compatible_wide_offset_and_comma_fraction() -> None:
    """Parse the legal RFC3339 forms Go accepts on provider responses."""
    assert _timestamp("2026-11-01T12:00:00,5+99:59") == datetime(2026, 10, 28, 8, 1, 0, 500000, UTC)


def test_wrong_typed_provider_field_is_invalid() -> None:
    """Reject wrong JSON types before constructing a persisted event."""
    with pytest.raises(ValueError):
        _event({
            "uri": "https://api.calendly.com/scheduled_events/EVT00001",
            "status": "active",
            "start_time": "2026-11-01T12:00:00Z",
            "end_time": "2026-11-01T12:30:00Z",
            "name": 42,
        })
