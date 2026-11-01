# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """Mixpanel connection settings from the manifest."""

    project_id: int
    base_url: str = "https://api.mixpanel.com"
    export_base_url: str = "https://data.mixpanel.com/api/2.0"
    timeout_seconds: float = 15
    max_response_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.mixpanel.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """Mixpanel provider credentials resolved per call."""

    service_account_username: SecretString
    service_account_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.mixpanel.client')
