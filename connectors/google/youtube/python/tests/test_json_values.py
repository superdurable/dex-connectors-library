# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Reject JSON numbers Go refuses to decode."""

import json

import pytest

from dex_connectors.google_youtube.json_values import strict_json_loads


@pytest.mark.parametrize("body", [b"NaN", b"Infinity", b"-Infinity", b"1e999"])
def test_nonfinite_json_number_is_rejected(body: bytes) -> None:
    with pytest.raises(json.JSONDecodeError):
        strict_json_loads(body)
