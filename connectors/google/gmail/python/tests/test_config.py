# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail Dex Web configuration field decoding and manifest defaults."""

import pytest

from dex_connectors.google_gmail import Config


def test_empty_settings_use_manifest_defaults() -> None:
    """A new connection needs no user-entered provider constants."""
    assert Config.from_project_settings({}) == Config()
    assert Config.from_project_settings({"endpoint": "", "maxResponseBytes": 0}) == Config()


def test_project_settings_decode_camel_case_and_duration_nanoseconds() -> None:
    """Dex Web's wire values become typed response bounds and poll timing."""
    config = Config.from_project_settings(
        {
            "endpoint": "https://gateway.example/gmail/v1",
            "maxResponseBytes": 2_000_000,
            "maxMessageBytes": 3_000_000,
            "pollInterval": 30_000_000_000,
            "pollPageSize": 50,
        }
    )
    assert config.endpoint == "https://gateway.example/gmail/v1"
    assert config.max_response_bytes == 2_000_000
    assert config.max_message_bytes == 3_000_000
    assert config.poll_interval_seconds == 30
    assert config.poll_page_size == 50


@pytest.mark.parametrize(
    "settings",
    [
        {"endpoint": "http://evil.example"},
        {"maxResponseBytes": -1},
        {"maxMessageBytes": -1},
        {"pollInterval": -1},
        {"pollPageSize": 101},
        {"maxResponseBytes": True},
        {"unexpected": "value"},
    ],
)
def test_invalid_project_settings_are_rejected(settings: dict[str, object]) -> None:
    """A connection cannot introduce an unsafe endpoint or invalid response bound."""
    with pytest.raises(ValueError):
        Config.from_project_settings(settings)
