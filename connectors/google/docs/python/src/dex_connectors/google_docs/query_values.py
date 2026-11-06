# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go-compatible URL query encoding for provider requests."""

from collections.abc import Mapping, Sequence
from urllib.parse import quote_plus


def encode_query_values(values: Mapping[str, str | Sequence[str]]) -> str:
    """Encode byte-sorted keys and ordered values like Go url.Values.Encode."""
    pairs = []
    for key in sorted(values, key=lambda item: item.encode("utf-8")):
        entry = values[key]
        for value in (entry,) if isinstance(entry, str) else entry:
            pairs.append(f"{quote_plus(key, safe='~')}={quote_plus(value, safe='~')}")
    return "&".join(pairs)
