# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Jira path, date, email, and provider response invariants."""

from datetime import UTC, datetime

import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.atlassian_jira_service_management import Client, Config, Credentials
from dex_connectors.atlassian_jira_service_management.client import Exchange, path_escape, query_escape
from dex_connectors.atlassian_jira_service_management.operations import _email, _jira_time, _page


def test_go_path_and_query_escaping() -> None:
    """Preserve Go's different path and query escaping rules."""
    special = "a b/!'()*~,;?$&+=:@"
    assert path_escape(special) == "a%20b%2F%21%27%28%29%2A~%2C%3B%3F$&+=:@"
    assert query_escape("a b*~") == "a+b%2A%7E"


@pytest.mark.parametrize("address", ["é@example.com", "foo@é.com", "x@[127.0.0.1]"])
def test_email_accepts_go_supported_addresses(address: str) -> None:
    """Keep international addresses and domain literals accepted by Go."""
    assert _email(address) == address


@pytest.mark.parametrize("address", ["foo..bar@example.com", "x@example..com", "Jane <jane@example.com>"])
def test_email_rejects_non_bare_or_malformed_addresses(address: str) -> None:
    """Reject display names and invalid dot atoms before provider dispatch."""
    with pytest.raises(ValueError):
        _email(address)


@pytest.mark.parametrize(
    "timestamp", ["2026-02-30T01:00:00Z", "2025-02-29T01:00:00Z", "2026-01-01T24:00:00Z"]
)
def test_jira_time_rejects_invalid_calendar_values(timestamp: str) -> None:
    """Reject dates that Go time.Parse rejects."""
    with pytest.raises(ValueError):
        _jira_time(timestamp)


def test_jira_time_accepts_comma_fraction_and_large_offset() -> None:
    """Parse the fractional separator and large offset accepted by Go."""
    assert _jira_time("2026-03-01T01:02:03,5+9900") == datetime(2026, 2, 24, 22, 2, 3, 500000, UTC)


def test_page_rejects_wrong_typed_provider_values() -> None:
    """Reject malformed page lists instead of silently coercing them."""
    with pytest.raises(ValueError):
        _page(Exchange(status=200, body=b'{"values":"not an array"}'))


def test_client_defaults_response_limit_and_validates_site() -> None:
    """Use Go's zero default while rejecting a malformed cloud ID."""
    reference = ConnectionRef("atlassian", "test")
    credentials = StaticCredentialProvider(
        {
            reference: Credentials(
                "client",
                SecretString("secret"),
                SecretString("token"),
                SecretString("refresh"),
            )
        }
    )
    client = Client(
        Config(cloud_id="11223344-a1b2-4b33-8c44-def123456789", max_response_bytes=0), credentials
    )
    assert client.max_response_bytes == 4194304
    with pytest.raises(ValueError):
        Client(Config(cloud_id="not-a-uuid"), credentials)
