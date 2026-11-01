# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """Cohere connection settings from the manifest."""

    base_url: str = "https://api.cohere.com"
    timeout_seconds: float = 30
    max_response_bytes: int = 4194304

setattr(Config, '__module__', 'dex_connectors.cohere.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """Cohere provider credentials resolved per call."""

    api_key: SecretString

setattr(Credentials, '__module__', 'dex_connectors.cohere.client')
