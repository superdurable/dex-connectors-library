# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused YouTube decoder and query compatibility checks."""

from datetime import UTC, datetime

import pytest

from dex_connectors.google_youtube.client import _count, _decode_search, _time


def test_go_compatible_rfc3339_boundaries() -> None:
    """Reject invalid calendar times and accept Go's wide offset and comma fraction."""
    assert _time("2026-01-01T00:00:00,5+99:59") == datetime(2025, 12, 27, 20, 1, 0, 500000, UTC)
    for invalid in ("2026-02-30T01:00:00Z", "2025-02-29T01:00:00Z", "2026-01-01T24:00:00Z"):
        with pytest.raises(ValueError):
            _time(invalid)


def test_wrong_typed_provider_fields_are_invalid() -> None:
    """Reject malformed provider values instead of coercing them into public models."""
    with pytest.raises(ValueError):
        _count({"viewCount": 12}, "viewCount")
    with pytest.raises(ValueError):
        _decode_search(b'{"items":[],"pageInfo":{"totalResults":"one"}}', 5)
