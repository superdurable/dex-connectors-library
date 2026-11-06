# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Local validation and provider decoding boundaries."""

from datetime import UTC, datetime

import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

import dex_connectors.google_drive as drive
from dex_connectors.google_drive.operations import _file, _timestamp


@pytest.mark.parametrize(
    "value",
    [
        "2026-02-30T00:00:00Z",
        "2025-02-29T00:00:00Z",
        "2026-01-01T24:00:00Z",
        "2026-01-01T00:00:00+00:60",
        "2026-01-01T00:00:00+99:59",
    ],
)
def test_rejects_invalid_go_timestamps(value: str) -> None:
    """Reject calendar dates and offsets Go's JSON decoder rejects."""
    with pytest.raises(ValueError):
        _timestamp(value)


def test_accepts_comma_fraction_and_offset() -> None:
    """Decode Go's JSON timestamp fractions and valid timezone offsets."""
    assert _timestamp("2026-01-05T04:00:00,5+23:59") == datetime(2026, 1, 4, 4, 1, 0, 500000, tzinfo=UTC)


def test_wrong_typed_drive_resource_fails() -> None:
    """Reject a malformed provider field before it reaches a persisted Result."""
    with pytest.raises(ValueError):
        _file({"id": "file_one", "name": "File", "mimeType": "text/plain", "owners": "someone"})


def test_client_validates_limits() -> None:
    """Reject invalid configured limits during construction."""
    reference = ConnectionRef("google", "drive-files")
    provider = StaticCredentialProvider(
        {reference: drive.Credentials(access_token=SecretString("inert-token"))}
    )
    with pytest.raises(ValueError):
        drive.Client(drive.Config(max_upload_bytes=5_242_881), provider)
