# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Boundary cases that the recorded provider responses do not cover."""

import pytest

from dex_connectors.google_calendar.client import path_escape
from dex_connectors.google_calendar.models import EventAttendeeInput
from dex_connectors.google_calendar.operations import _attendees, _instant


@pytest.mark.parametrize(
    "value",
    [
        "2026-02-30T09:00:00Z",
        "2025-02-29T09:00:00Z",
        "2026-02-24T24:00:00Z",
        "2026-02-24T09:00:00",
    ],
)
def test_invalid_rfc3339_instants_are_rejected(value: str) -> None:
    """Reject calendar dates and local times that Go time.Parse rejects."""
    with pytest.raises(ValueError):
        _instant("timeMin", value)


def test_go_offset_and_comma_fraction_are_accepted() -> None:
    """Accept the extended offset and comma fraction supported by Go."""
    assert _instant("timeMin", "2026-02-24T09:00:00,5+99:59").microsecond == 500000


def test_instant_rejects_unrepresentable_nanoseconds() -> None:
    with pytest.raises(ValueError, match="timeMin"):
        _instant("timeMin", "2026-02-24T09:00:00.123456789Z")


def test_go_path_escape_and_unicode_mail_addresses() -> None:
    """Keep provider IDs escaped and accept Go-supported bare addresses."""
    assert path_escape("a/b!?'(),;~$&+=:@") == "a%2Fb%21%3F%27%28%29%2C%3B~$&+=:@"
    _attendees((EventAttendeeInput("δοκιμή@παράδειγμα.δοκιμή"), EventAttendeeInput("user@[127.0.0.1]")))
    with pytest.raises(ValueError):
        _attendees((EventAttendeeInput("Guest <guest@example.com>"),))
