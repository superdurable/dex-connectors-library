# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Validate Zoom's response and request boundaries beyond recorded happy paths."""

import json

import pytest

from dex_connectors.zoom.meeting import decode_meeting_body, start_instant


@pytest.mark.parametrize(
    "value",
    [
        {"id": 1, "duration": "30"},
        {"id": 1, "settings": {"host_video": "yes"}},
        {"id": 1, "join_url": "http://zoom.example/j/1"},
        {"id": 0},
    ],
)
def test_invalid_meeting_resource(value: dict[str, object]) -> None:
    """Reject wrong provider types, unsafe URLs, and missing numeric IDs."""
    with pytest.raises(ValueError):
        decode_meeting_body(json.dumps(value).encode())


@pytest.mark.parametrize(
    "value",
    [
        "2036-02-30T17:00:00Z",
        "2035-02-29T17:00:00Z",
        "2036-02-24T24:00:00Z",
        "2036-02-24T17:00:00",
        "2036-02-24T17:00:00.1Z",
    ],
)
def test_invalid_start_instant(value: str) -> None:
    """Reject invalid calendar dates, missing offsets, and fractional seconds."""
    with pytest.raises(ValueError):
        start_instant(value, "America/Los_Angeles")


def test_zoom_accepts_extended_offsets_and_comma_zero_fraction() -> None:
    """Match Go time.Parse on uncommon but valid RFC 3339 offset spellings."""
    from datetime import UTC, datetime

    assert start_instant("2036-02-24T17:00:00+99:59", "America/Los_Angeles") == datetime(
        2036, 2, 20, 13, 1, tzinfo=UTC
    )
    assert start_instant("2036-02-24T17:00:00,0000000001Z", "UTC") == datetime(2036, 2, 24, 17, 0, tzinfo=UTC)
