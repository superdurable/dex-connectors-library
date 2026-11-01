# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """PayPal connection settings from the manifest."""

    base_url: str = "https://api-m.paypal.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.paypal.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """PayPal provider credentials resolved per call."""

    client_id: str
    client_secret: SecretString

setattr(Credentials, '__module__', 'dex_connectors.paypal.client')
