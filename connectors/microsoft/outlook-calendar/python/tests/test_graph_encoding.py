# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go-derived query, path, and timestamp edge cases."""

import pytest

from dex_connectors.microsoft_outlook_calendar.client import (
    _display,
    _instant,
    _path_escape,
    _query,
    _returned_boundary,
)


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
