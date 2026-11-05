# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused input and provider-field validation parity checks."""

from datetime import UTC, datetime
from functools import cmp_to_key

import pytest

from dex_connectors.intercom.client import _error_summary
from dex_connectors.intercom.models import ConversationPart
from dex_connectors.intercom.operations import (
    _bare_email,
    _compare_parts_newest_first,
    _rfc3339,
    _time,
    _validate_conversation_wire,
)


@pytest.mark.parametrize("address", ["ada@example.com", "δοκιμή@παράδειγμα.δοκιμή", "user@[127.0.0.1]"])
def test_go_accepted_bare_email_addresses(address: str) -> None:
    """Allow international addresses and IPv4 literals accepted by Go."""
    assert _bare_email(address)


@pytest.mark.parametrize("address", ["user@bad..com", "user@[IPv6:2001:db8::1]", "Ada <ada@example.com>"])
def test_go_rejected_bare_email_addresses(address: str) -> None:
    """Reject malformed domains and display names before provider calls."""
    assert not _bare_email(address)


@pytest.mark.parametrize("instant", ["2026-02-30T00:00:00Z", "2025-02-29T00:00:00Z", "2026-01-01T24:00:00Z"])
def test_invalid_calendar_instants(instant: str) -> None:
    """Reject dates that Go time.Parse rejects."""
    assert _rfc3339(instant) is None


def test_extended_offset_and_comma_fraction() -> None:
    """Go accepts comma fractions and offset hours through 24 only."""
    assert _rfc3339("2026-01-01T00:00:00,1+24:00") is not None
    assert _rfc3339("2026-01-01T00:00:00,1+25:00") is None


@pytest.mark.parametrize("offset", ["+00:60", "+24:59", "-00:60", "+24:60"])
def test_go_accepted_offset_minutes(offset: str) -> None:
    """Go time.Parse permits offset minutes through 60."""
    assert _rfc3339("2026-01-01T00:00:00" + offset) is not None


@pytest.mark.parametrize("offset", ["+00:61", "+25:00"])
def test_go_rejected_offsets(offset: str) -> None:
    """Reject offsets beyond Go time.Parse limits."""
    assert _rfc3339("2026-01-01T00:00:00" + offset) is None


def test_wrong_typed_provider_timestamp() -> None:
    """Reject a provider timestamp that Go cannot decode."""
    with pytest.raises(ValueError):
        _time(1.2)


def test_provider_wire_rejects_unused_wrong_typed_fields() -> None:
    """Decode the complete Go wire shape before selecting response fields."""
    with pytest.raises(ValueError):
        _validate_conversation_wire({"id": "123", "title": 5})
    with pytest.raises(ValueError):
        _validate_conversation_wire(
            {"id": "123", "conversation_parts": {"conversation_parts": [{"body": 5}]}}
        )


def test_part_sort_matches_go_for_mixed_and_leading_zero_ids() -> None:
    """Keep Go's textual fallback and length-first numeric comparison."""
    instant = datetime(2026, 1, 1, tzinfo=UTC)
    parts = [
        ConversationPart(id=part_id, part_type="comment", created_at=instant)
        for part_id in ("123", "abc", "00123", "9")
    ]
    assert [part.id for part in sorted(parts, key=cmp_to_key(_compare_parts_newest_first))] == [
        "abc",
        "00123",
        "123",
        "9",
    ]


def test_unrepresentable_provider_timestamp_names_field() -> None:
    """A Go timestamp outside Python's datetime range is terminal."""
    with pytest.raises(ValueError, match="created_at"):
        _time(2**63 - 1, "created_at")


def test_error_summary_collects_three_valid_codes() -> None:
    """Invalid codes do not consume Go's three-code limit."""
    summary, request_id = _error_summary(
        b'{"request_id":"request-1","errors":['
        b'{"code":"INVALID","field":"first"},'
        b'{"code":"one"},{"code":"two"},{"code":"three"},{"code":"four"}]}'
    )
    assert summary == "one, two, three; fields: first"
    assert request_id == "request-1"


def test_error_summary_discards_wrong_typed_fields() -> None:
    """Go discards the entire decoded summary on a struct field type error."""
    assert _error_summary(b'{"request_id":"request-1","errors":[{"code":"one","field":4}]}') == (
        "",
        "",
    )


def test_error_summary_keeps_valid_fields_around_json_nulls() -> None:
    """JSON null leaves Go string fields at their zero value."""
    assert _error_summary(
        b'{"request_id":null,"errors":['
        b'{"code":null,"field":"admin_id"},'
        b'{"code":"not_found","field":null}]}'
    ) == ("not_found; fields: admin_id", "")
