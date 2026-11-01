# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:
    """Shopify connection settings from the manifest."""

    shop_subdomain: str
    timeout_seconds: float = 15
    max_response_bytes: int = 2097152

setattr(Config, '__module__', 'dex_connectors.shopify.client')


@dataclass(frozen=True, slots=True)
class Credentials:
    """Shopify provider credentials resolved per call."""

    access_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.shopify.client')
