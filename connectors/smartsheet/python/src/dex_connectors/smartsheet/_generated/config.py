# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """Smartsheet connection settings from the manifest."""

    base_url: str = "https://api.smartsheet.com/2.0"
    timeout_seconds: float = 30
    max_response_bytes: int = 4194304

setattr(Config, '__module__', 'dex_connectors.smartsheet.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """Smartsheet provider credentials resolved per call."""

    api_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.smartsheet.client')
