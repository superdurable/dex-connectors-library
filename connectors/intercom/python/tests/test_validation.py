# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused input and provider-field validation parity checks."""

import pytest

from dex_connectors.intercom.operations import _bare_email, _rfc3339, _time


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
    """Accept Go's extended numeric offset and comma fractions."""
    assert _rfc3339("2026-01-01T00:00:00,1+99:59") is not None


def test_wrong_typed_provider_timestamp() -> None:
    """Reject a provider timestamp that Go cannot decode."""
    with pytest.raises(ValueError):
        _time(1.2)
