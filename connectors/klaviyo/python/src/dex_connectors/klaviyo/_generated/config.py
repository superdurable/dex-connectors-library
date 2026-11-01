# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """Klaviyo connection settings from the manifest."""

    base_url: str = "https://a.klaviyo.com/api"
    timeout_seconds: float = 15
    max_response_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.klaviyo.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """Klaviyo provider credentials resolved per call."""

    private_api_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.klaviyo.client')
