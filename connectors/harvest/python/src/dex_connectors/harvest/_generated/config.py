# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """Harvest connection settings from the manifest."""

    account_id: str
    base_url: str = "https://api.harvestapp.com/v2"
    timeout_seconds: float = 30
    max_response_bytes: int = 4194304

setattr(Config, '__module__', 'dex_connectors.harvest.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """Harvest provider credentials resolved per call."""

    api_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.harvest.client')
