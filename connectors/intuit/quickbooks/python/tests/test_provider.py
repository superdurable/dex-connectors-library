# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check provider input boundaries not covered by the wire recordings."""

import base64
import json
from datetime import UTC, datetime

import httpx
import pytest
from dex_connectors.sdk import SecretString

from dex_connectors.intuit_quickbooks import Credentials
from dex_connectors.intuit_quickbooks.client import _classify, _resolve_realm
from dex_connectors.intuit_quickbooks.provider import (
    decode_invoice,
    parse_timestamp,
    validate_date,
    validate_email,
    wire_decimal,
)


@pytest.mark.parametrize("value", ["2026-02-30", "2025-02-29", "2026-01-01T24:00:00Z"])
def test_calendar_date_rejects_impossible_values(value: str) -> None:
    """Reject impossible calendar dates before a provider request."""
    with pytest.raises(ValueError):
        validate_date("txnDate", value)


def test_wrong_typed_invoice_fields_are_invalid() -> None:
    """Refuse provider object fields whose Go type is a string."""
    with pytest.raises(ValueError):
        decode_invoice({"Id": 130, "CustomerRef": {"value": "58"}, "TotalAmt": 10, "Balance": 0})


def test_non_ascii_email_and_domain_literal_follow_go() -> None:
    """Preserve RFC addresses that Go's parser accepts."""
    assert validate_email("emailAddress", "é@example.com") == "é@example.com"
    assert validate_email("emailAddress", "jane@[127.0.0.1]") == "jane@[127.0.0.1]"


def test_large_error_body_still_classifies_by_status() -> None:
    """Ignore the success-response size limit when QuickBooks returns a 503."""
    result = _classify("getInvoice", 503, httpx.Headers(), b"x" * 1000, 5, "", datetime.now(UTC))
    assert result.outcome == "retry"


def test_wire_exponent_preserves_decimal_value() -> None:
    """Expand a provider JSON exponent without converting through a float."""
    assert wire_decimal("1.5e2") == "150"


def test_go_rfc3339_accepts_large_offset_and_comma_fraction() -> None:
    """Match Go time.Parse on provider timestamps beyond Python timezone bounds."""
    assert parse_timestamp("2026-10-01T12:30:00,5+99:59") == datetime(2026, 9, 27, 8, 31, 0, 500000, UTC)
    with pytest.raises(ValueError):
        parse_timestamp("2026-02-30T24:00:00Z")


@pytest.mark.parametrize("claim_name", ["realmid", "realmId"])
def test_id_token_realm_claim_spellings_match_go(claim_name: str) -> None:
    """Accept both Intuit claim spellings used by the Go connector."""
    claims = {
        "iss": "https://oauth.platform.intuit.com/op/v1",
        "aud": "test-client",
        claim_name: "9341453050298464",
    }
    payload = base64.urlsafe_b64encode(json.dumps(claims).encode()).decode().rstrip("=")
    credentials = Credentials(
        "test-client",
        SecretString("secret"),
        SecretString("access"),
        SecretString("refresh"),
        SecretString(f"header.{payload}.signature"),
    )
    assert _resolve_realm(credentials, "") == "9341453050298464"
