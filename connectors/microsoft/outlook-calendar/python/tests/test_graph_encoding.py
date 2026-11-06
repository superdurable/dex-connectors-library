# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go-derived query, path, and timestamp edge cases."""

import pytest

from dex_connectors.microsoft_outlook_calendar import Config
from dex_connectors.microsoft_outlook_calendar.client import (
    Client,
    _display,
    _event_times,
    _instant,
    _is_bare_address,
    _path_escape,
    _query,
    _returned_boundary,
    _validate_attendees,
)
from dex_connectors.microsoft_outlook_calendar.models import EventAttendeeInput, EventDateTime


@pytest.mark.parametrize(
    ("value", "query", "path"),
    [
        ("a b", "a=a%20b&z=a%20b", "a%20b"),
        ("x~*!'()", "a=x~%2A%21%27%28%29&z=x~%2A%21%27%28%29", "x~%2A%21%27%28%29"),
        ("calendar/id?=x+y", "a=calendar%2Fid%3F%3Dx%2By&z=calendar%2Fid%3F%3Dx%2By", "calendar%2Fid%3F=x+y"),
        ("é", "a=%C3%A9&z=%C3%A9", "%C3%A9"),
    ],
)
def test_go_url_values_and_path_escape(value: str, query: str, path: str) -> None:
    """Match Go url.Values.Encode with Graph's space override and url.PathEscape."""
    assert _query({"z": value, "a": value}) == query
    assert _path_escape(value) == path


@pytest.mark.parametrize(
    "value", ["2026-02-24T24:00:00Z", "2026-02-24T17:00:00", "2026-02-24T17:00:00.1234567Z"]
)
def test_non_round_trippable_instant_fails_with_field(value: str) -> None:
    """Reject values Python cannot preserve or Go time.Parse rejects."""
    with pytest.raises(ValueError, match="start.dateTime"):
        _instant("start.dateTime", value)


def test_graph_wall_time_accepts_zero_seventh_fraction() -> None:
    """Graph's common seven-digit zero fraction preserves Go's second resolution."""
    value, _instant_value = _returned_boundary(
        "start", {"dateTime": "2026-02-24T17:00:00.0000000", "timeZone": "UTC"}, _display("UTC")
    )
    assert value.date_time == "2026-02-24T17:00:00Z"


@pytest.mark.parametrize(
    "address",
    ("a.@x.com", "jane@x.com.", '"q"@x.com', "a..b@x.com", "x(c)@y.com"),
)
def test_go_rejects_non_bare_attendee_and_mailbox(address: str) -> None:
    assert not _is_bare_address(address)
    with pytest.raises(ValueError):
        _validate_attendees((EventAttendeeInput(email=address),))
    with pytest.raises(ValueError):
        Client(Config(mailbox=address), object())  # type: ignore[arg-type]


def test_go_accepts_unicode_addr_spec() -> None:
    assert _is_bare_address("é@bücher.de")


def test_all_day_rejects_ambiguous_havana_midnight() -> None:
    with pytest.raises(ValueError, match="ambiguous midnight"):
        _event_times(
            EventDateTime("2024-11-03T00:00:00-04:00", "America/Havana"),
            EventDateTime("2024-11-04T00:00:00-05:00", "America/Havana"),
            True,
        )


@pytest.mark.parametrize("url", ("https://example.com", "http://127.0.0.1:12345/v1.0"))
def test_local_provider_url_requires_loopback_root(url: str) -> None:
    with pytest.raises(ValueError):
        Client(Config(), object(), local_provider_url=url)  # type: ignore[arg-type]
