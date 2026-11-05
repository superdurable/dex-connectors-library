# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Help Scout credential, timestamp, and query edge cases outside fixture replay."""

from datetime import UTC, datetime
from typing import cast

import dex_connectors.sdk as sdk
import httpx
import pytest

from dex_connectors.helpscout import Client, Config, Credentials
from dex_connectors.helpscout.client import _query_escape
from dex_connectors.helpscout.decode import canonical_timestamp, parse_timestamp
from dex_connectors.helpscout.operations import _bare_email


class _FailingCredentials:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> Credentials:
        raise self.error


@pytest.mark.parametrize(
    "error,expected",
    [
        (sdk.ReauthorizationRequiredError(), "terminal"),
        (OSError("token endpoint unavailable"), "retry"),
    ],
)
async def test_credential_failure_classification(error: Exception, expected: str) -> None:
    """Keep rejected app credentials terminal and token outages retryable."""
    async with httpx.AsyncClient() as transport:
        client = Client(
            Config(), cast(sdk.CredentialProvider[Credentials], _FailingCredentials(error)), transport
        )
        token, decision = await client.resolve_token(cast(sdk.Call, object()))
        assert token == "" and decision == expected


@pytest.mark.parametrize(
    "value",
    [
        "2026-02-30T12:00:00Z",
        "2025-02-29T12:00:00Z",
        "2026-01-01T24:00:00Z",
        "2026-01-01T12:00:00+01:60",
    ],
)
def test_invalid_rfc3339_is_rejected(value: str) -> None:
    """Reject calendar and clock values that Go time.Parse refuses."""
    with pytest.raises(ValueError):
        parse_timestamp(value)


def test_go_timestamp_accepts_comma_fraction_and_wide_offset() -> None:
    """Accept Go's comma fraction and an offset beyond Python tzinfo bounds."""
    assert parse_timestamp("2026-03-01T12:34:56,25+99:59") == datetime(
        2026, 2, 25, 8, 35, 56, 250000, tzinfo=UTC
    )
    assert canonical_timestamp("2026-03-01T12:34:56,123456789+99:59") == (
        "2026-02-25T08:35:56.123456789Z"
    )


def test_go_query_escape_encodes_tilde_and_spaces() -> None:
    """Use Go QueryEscape for arbitrary tag and email filter characters."""
    assert _query_escape("x~ *+") == "x%7E+%2A%2B"


@pytest.mark.parametrize(
    "address,accepted",
    [
        ("jane@[127.0.0.1]", True),
        ("jane@acme.example.com", True),
        ("Jäne@bücher.example", True),
        ("Display <jane@example.com>", False),
        ("jane@example.com (comment)", False),
    ],
)
def test_bare_email_matches_go(address: str, accepted: bool) -> None:
    """Preserve Go's domain literal and internationalized bare-address behavior."""
    assert _bare_email(address) is accepted


def test_zero_config_uses_manifest_defaults() -> None:
    """A zero-valued direct Config has the same effective limits as Go."""
    client = Client(
        Config(0, 0),
        sdk.StaticCredentialProvider({}),
    )
    assert client.config.max_response_bytes == Config().max_response_bytes
    assert client.config.webhook_max_body_bytes == Config().webhook_max_body_bytes
