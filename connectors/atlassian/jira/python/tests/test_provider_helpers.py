# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider wire rules that require exact Go-compatible encoding."""

from datetime import UTC, datetime

import pytest

from dex_connectors.atlassian_jira.client import path_escape, query_escape
from dex_connectors.atlassian_jira.operations import _equal_fold, _exact_json_value, _issue_json, _timestamp


@pytest.mark.parametrize(
    ("value", "escaped"),
    [
        ("OPS-42", "OPS-42"),
        ("a!b'c(d)e*f,g;h?i/j", "a%21b%27c%28d%29e%2Af%2Cg%3Bh%3Fi%2Fj"),
        ("$&+=:@~", "$&+=:@~"),
    ],
)
def test_path_escape_matches_go(value: str, escaped: str) -> None:
    assert path_escape(value) == escaped


def test_query_escape_matches_go_values_encode() -> None:
    assert query_escape("a b*~+/&") == "a+b%2A~%2B%2F%26"


@pytest.mark.parametrize(
    "value", ["2026-02-30T00:00:00Z", "2025-02-29T00:00:00Z", "2026-01-01T24:00:00Z", "2026-01-01 01:00:00Z"]
)
def test_timestamp_rejects_invalid_dates(value: str) -> None:
    with pytest.raises(ValueError):
        _timestamp(value)


def test_timestamp_accepts_go_offsets_and_comma_fraction() -> None:
    assert _timestamp("2026-02-28T01:02:03,5+99:59") == datetime(2026, 2, 23, 21, 3, 3, 500000, tzinfo=UTC)
    assert _timestamp("2024-02-29T01:02:03.000-0700") == datetime(2024, 2, 29, 8, 2, 3, tzinfo=UTC)


def test_timestamp_rejects_unrepresentable_nanoseconds() -> None:
    with pytest.raises(ValueError, match="timestamp"):
        _timestamp("2026-01-01T01:02:03.123456789Z")


def test_additional_field_numbers_cannot_be_rounded_or_rewritten() -> None:
    for number in (b"1.10", b"1e400"):
        with pytest.raises(ValueError, match="exactly"):
            _exact_json_value(_issue_json(number))
    with pytest.raises(ValueError):
        _issue_json(b"NaN")


def test_equal_fold_does_not_expand_characters() -> None:
    assert _equal_fold("Straße", "STRAẞE")
    assert not _equal_fold("Straße", "STRASSE")
