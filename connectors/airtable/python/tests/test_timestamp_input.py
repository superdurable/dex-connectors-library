# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Generated datetime inputs match Go JSON when Python can preserve them."""

import json

import pytest
from dex import CodecRegistry

from dex_connectors.airtable.models import Record


@pytest.mark.parametrize(
    ("supplied", "canonical"),
    [
        ("2026-11-05T12:34:56,123Z", "2026-11-05T12:34:56.123Z"),
        ("2026-11-05T12:34:56+00:60", "2026-11-05T12:34:56+01:00"),
        ("2026-11-05T1:04:05Z", "2026-11-05T01:04:05Z"),
        ("2026-11-05T12:34:56.1234560009Z", "2026-11-05T12:34:56.123456Z"),
    ],
)
def test_created_time_encodes_like_go(supplied: str, canonical: str) -> None:
    record = Record(id="record-1", created_time=supplied)  # type: ignore[arg-type]
    encoded = CodecRegistry().resolve(Record).encode(record)
    assert json.loads(encoded.data)["createdTime"] == canonical


@pytest.mark.parametrize(
    "supplied",
    [
        "2026-11-05T12:34:56.1234567Z",
        "0000-01-01T00:00:00Z",
        "2026-11-05T12:34:56+24:00",
        "2026-11-05t12:34:56Z",
    ],
)
def test_created_time_fails_with_field_name_when_not_representable(supplied: str) -> None:
    with pytest.raises(ValueError, match="created_time"):
        Record(id="record-1", created_time=supplied)  # type: ignore[arg-type]
