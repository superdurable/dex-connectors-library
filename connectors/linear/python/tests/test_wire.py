# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check provider parsing and stable client IDs at input boundaries."""

import pytest
from dex_connectors.sdk import OperationRef, derive_call_id

from dex_connectors.linear.wire import client_entity_id, due_date, email, issue, issue_summary, linear_time


def test_client_entity_id_uses_stable_call_identity() -> None:
    """Keep a retry's issue UUID identical and distinct from a comment UUID."""
    call_id = derive_call_id("linear-flow", "conformance", OperationRef("linear", "createIssue"))
    issue_id = client_entity_id(str(call_id), "issue")
    assert issue_id == "7a829188-ced1-400a-819b-dc454c738a04"
    assert client_entity_id(str(call_id), "issue") == issue_id
    assert client_entity_id(str(call_id), "comment") != issue_id


@pytest.mark.parametrize("invalid", ["2026-02-30", "2025-02-29", "2026-13-01", "2026-2-9"])
def test_due_date_rejects_non_calendar_dates(invalid: str) -> None:
    """Do not send dates Go would reject locally."""
    with pytest.raises(ValueError):
        due_date(invalid)


def test_linear_time_accepts_go_wide_offset_and_comma_fraction() -> None:
    """Normalize Go accepted offsets and fractional seconds before generated model decoding."""
    assert linear_time("2026-10-26T09:00:00,123456789+99:59") == "2026-10-22T05:01:00.123456789Z"


@pytest.mark.parametrize("invalid", ["2026-02-30T00:00:00Z", "2025-02-29T00:00:00Z", "2026-01-01T24:00:00Z"])
def test_linear_time_rejects_invalid_date_time(invalid: str) -> None:
    """Reject dates that Go time.Parse rejects."""
    with pytest.raises(ValueError):
        linear_time(invalid)


def test_email_matches_linear_go_shape_check() -> None:
    """Permit Unicode and domain literals only when Go's connector check permits them."""
    assert email("  café@example.com  ") == "café@example.com"
    assert email("name@[example.com]") == "name@[example.com]"
    with pytest.raises(ValueError):
        email("person@localhost")


def test_issue_summary_rejects_wrong_typed_provider_field() -> None:
    """A malformed priority must select invalidResponse, not become an integer."""
    node = {
        "id": "44444444-4444-4444-8444-444444444444",
        "identifier": "ENG-210",
        "title": "Fire drill",
        "url": "https://linear.app/example",
        "team": {"id": "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4", "key": "ENG", "name": "Engineering"},
        "state": {"id": "22222222-2222-4222-8222-222222222222", "name": "Todo", "type": "unstarted"},
        "priority": "2",
        "createdAt": "2026-10-26T09:00:00Z",
        "updatedAt": "2026-10-26T09:00:00Z",
    }
    with pytest.raises(ValueError):
        issue_summary(node)
    for malformed_priority in (5.0, 1e3):
        with pytest.raises(ValueError):
            issue_summary({**node, "priority": malformed_priority})


@pytest.mark.parametrize("malformed_number", [5.0, 1e3])
def test_issue_rejects_non_integer_number(malformed_number: float) -> None:
    """Keep JSON floating literals out of Go integer fields."""
    node = {
        "id": "44444444-4444-4444-8444-444444444444",
        "identifier": "ENG-210",
        "team": {"id": "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"},
        "state": {"id": "22222222-2222-4222-8222-222222222222"},
        "createdAt": "2026-10-26T09:00:00Z",
        "updatedAt": "2026-10-26T09:00:00Z",
        "number": malformed_number,
    }
    with pytest.raises(ValueError):
        issue(node)
