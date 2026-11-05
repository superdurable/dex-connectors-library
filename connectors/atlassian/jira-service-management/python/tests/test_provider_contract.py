# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check provider-specific escaping, validation, and checkpoint wire values."""

import json
from datetime import UTC, datetime

import pytest
from dex import CodecRegistry
from dex_connectors.sdk import CallID

from dex_connectors.atlassian_jira_service_management.client import _query_escape, path_escape
from dex_connectors.atlassian_jira_service_management.operations import (
    _DispatchMarker,
    _email_address,
    _jira_time,
    _ticket,
)


@pytest.mark.parametrize(
    ("value", "escaped"),
    [
        ("A-Z_a~$&+=:@", "A-Z_a~$&+=:@"),
        ("!'()*,;? /", "%21%27%28%29%2A%2C%3B%3F%20%2F"),
        ("é", "%C3%A9"),
    ],
)
def test_path_escape_matches_go(value: str, escaped: str) -> None:
    """Keep provider paths equivalent to Go url.PathEscape."""
    assert path_escape(value) == escaped


@pytest.mark.parametrize(
    ("value", "escaped"),
    [
        ("a b~*+", "a+b~%2A%2B"),
        ("AZaz09-._~", "AZaz09-._~"),
        ("! '()*", "%21+%27%28%29%2A"),
        ("a&b=c/d?e", "a%26b%3Dc%2Fd%3Fe"),
        ("é 雪", "%C3%A9+%E9%9B%AA"),
    ],
)
def test_query_escape_matches_go_values(value: str, escaped: str) -> None:
    """Match values recorded from Go url.QueryEscape."""
    assert _query_escape(value) == escaped


@pytest.mark.parametrize("value", ["ü@example.com", "jane@exämple.com", "jane@[127.0.0.1]", "jane@[::1]"])
def test_email_accepts_go_bare_addresses(value: str) -> None:
    """Accept Unicode and IP address literal forms Go accepts."""
    assert _email_address(value) == value


@pytest.mark.parametrize(
    "value", ["jane..doe@example.com", "jane@foo..bar", "Jane <jane@example.com>", "jane@[IPv6:2001:db8::1]"]
)
def test_email_rejects_go_invalid_addresses(value: str) -> None:
    """Reject malformed or decorated customer addresses."""
    with pytest.raises(ValueError):
        _email_address(value)


@pytest.mark.parametrize("value", ["2026-02-30T10:00:00Z", "2025-02-29T10:00:00Z", "2026-01-01T24:00:00Z"])
def test_jira_date_rejects_invalid_calendar_values(value: str) -> None:
    """Reject impossible dates and times instead of silently rolling them over."""
    with pytest.raises(ValueError):
        _jira_time(value)


def test_jira_date_accepts_go_fraction_and_large_offset() -> None:
    """Accept Go's comma fraction and extended numeric offset."""
    assert _jira_time("2026-02-28T10:00:00,5+99:59") == datetime(2026, 2, 24, 6, 1, 0, 500000, UTC)


def test_dispatch_checkpoint_uses_go_wire_names() -> None:
    """Persist the same marker shape Go reads on a later attempt."""
    marker = _DispatchMarker(CallID("ab66bfde-d934-5d47-9e00-bdf172b83392"), 1)
    codec = CodecRegistry().resolve(_DispatchMarker)
    encoded = json.loads(codec.encode(marker).data)
    assert encoded == {"jiraServiceManagementDispatchedCallId": str(marker.dispatched_call_id), "attempt": 1}
    assert codec.decode(codec.encode(marker)) == marker


def test_wrong_typed_ticket_fields_fail_decoding() -> None:
    """Treat a provider array in the issue fields as an invalid response."""
    with pytest.raises(ValueError):
        _ticket({"id": "10042", "key": "ITH-42", "fields": []})
